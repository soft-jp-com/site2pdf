package pdfgen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
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
func setScreenshotViewport(ctx context.Context, cli *cdpClient, sid string, so ScreenshotOptions) error {
	_, err := cli.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             so.Width,
		"height":            so.TileHeight,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}, sid)
	return err
}

// captureScreenshots はページ全体を縦に分割してJPEGで撮る。
func captureScreenshots(ctx context.Context, cli *cdpClient, sid string, so ScreenshotOptions) (*Screenshots, error) {
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
