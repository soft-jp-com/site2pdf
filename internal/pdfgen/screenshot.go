package pdfgen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// ScreenshotOptions はスクリーンショット（縦に分割したJPEG）の設定。
// 画像を読むAIは長辺が長い画像を縮小して文字がつぶれるため、1枚の縦長画像ではなく分割して撮る。
type ScreenshotOptions struct {
	Width      int // 表示幅(px)
	TileHeight int // 1枚あたりの高さ(px)
	MaxTiles   int // 最大枚数（超えた分は撮らず Truncated にする）
	Quality    int // JPEG画質 (1-100)
}

// DefaultScreenshotOptions は既定のスクリーンショット設定を返す。
func DefaultScreenshotOptions() ScreenshotOptions {
	return ScreenshotOptions{Width: 1280, TileHeight: 1600, MaxTiles: 6, Quality: 70}
}

// ScreenshotTile は分割したスクリーンショット1枚。
type ScreenshotTile struct {
	Y      int    // ページ先頭からの位置(px)
	Height int    // 画像の高さ(px)
	JPEG   []byte // JPEGデータ
}

// Screenshots はページのスクリーンショット一式。
type Screenshots struct {
	Tiles      []ScreenshotTile
	PageHeight int  // ページ全体の高さ(px)
	Truncated  bool // MaxTiles を超えたため下の方を撮っていない
}

// tileLayout はページの高さから、撮る範囲(y, 高さ)の一覧と、切り捨てたかどうかを返す。
func tileLayout(pageHeight, tileHeight, maxTiles int) (tiles [][2]int, truncated bool) {
	if pageHeight <= 0 || tileHeight <= 0 {
		return nil, false
	}
	for y := 0; y < pageHeight; y += tileHeight {
		if maxTiles > 0 && len(tiles) >= maxTiles {
			return tiles, true
		}
		h := tileHeight
		if y+h > pageHeight {
			h = pageHeight - y
		}
		tiles = append(tiles, [2]int{y, h})
	}
	return tiles, false
}

// setScreenshotViewport はスクリーンショット用に表示幅を固定する。ページ遷移の前に呼ぶ。
// あわせて通知の許可を「拒否」にしておき、「通知を受け取りますか」系のお誘いを出さないサイトを増やす。
func setScreenshotViewport(ctx context.Context, cli *cdpClient, sid string, so ScreenshotOptions) error {
	// ブラウザ単位の設定なのでセッションIDなしで送る。失敗しても撮影自体はできるので無視する
	_, _ = cli.send(ctx, "Browser.setPermission", map[string]any{
		"permission": map[string]any{"name": "notifications"},
		"setting":    "denied",
	}, "")
	_, err := cli.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             so.Width,
		"height":            so.TileHeight,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}, sid)
	return err
}

// hideFixedElementsJS は画面に固定された要素（通知のお誘い・Cookie同意・追従ヘッダー等）を撮影前に隠すJS。
// 分割撮影では position: fixed の要素が各タイルの同じ位置に描かれて本文を覆うため非表示にする
// （ただし本文を丸ごと包んでいる大きな要素は残す）。
// position: sticky はスクロール位置0のまま撮るので本来の位置に1回だけ描かれる。static に変えると
// 周りのレイアウトが崩れるサイトがあった（朝日新聞の右列が重なった）ため触らない。
// 隠した要素の数を返す。
const hideFixedElementsJS = `(function(){
  var n = 0;
  var els = document.body ? document.body.querySelectorAll('*') : [];
  for (var i = 0; i < els.length; i++) {
    var el = els[i];
    var pos = getComputedStyle(el).position;
    if (pos === 'fixed') {
      if ((el.innerText || '').length > 2000) continue;
      el.style.setProperty('display', 'none', 'important');
      n++;
    }
  }
  return n;
})()`

// captureScreenshots はページ全体を縦に分割してJPEGで撮る。
func captureScreenshots(ctx context.Context, cli *cdpClient, sid string, so ScreenshotOptions) (*Screenshots, error) {
	_, _ = cli.eval(ctx, sid, hideFixedElementsJS)
	_ = waitCtx(ctx, 100*time.Millisecond)

	res, err := cli.send(ctx, "Page.getLayoutMetrics", nil, sid)
	if err != nil {
		return nil, fmt.Errorf("Page.getLayoutMetrics: %w", err)
	}
	var lm struct {
		CSSContentSize struct {
			Height float64 `json:"height"`
		} `json:"cssContentSize"`
	}
	if err := json.Unmarshal(res, &lm); err != nil {
		return nil, fmt.Errorf("レイアウト情報の解析: %w", err)
	}
	pageHeight := int(math.Ceil(lm.CSSContentSize.Height))

	layout, truncated := tileLayout(pageHeight, so.TileHeight, so.MaxTiles)
	shots := &Screenshots{PageHeight: pageHeight, Truncated: truncated}
	for _, t := range layout {
		res, err := cli.send(ctx, "Page.captureScreenshot", map[string]any{
			"format":  "jpeg",
			"quality": so.Quality,
			"clip": map[string]any{
				"x": 0, "y": t[0], "width": so.Width, "height": t[1], "scale": 1,
			},
			"captureBeyondViewport": true,
		}, sid)
		if err != nil {
			return nil, fmt.Errorf("Page.captureScreenshot(y=%d): %w", t[0], err)
		}
		var out struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(res, &out); err != nil {
			return nil, fmt.Errorf("スクリーンショット結果の解析: %w", err)
		}
		raw, err := base64.StdEncoding.DecodeString(out.Data)
		if err != nil {
			return nil, fmt.Errorf("スクリーンショットのデコード: %w", err)
		}
		shots.Tiles = append(shots.Tiles, ScreenshotTile{Y: t[0], Height: t[1], JPEG: raw})
	}
	return shots, nil
}
