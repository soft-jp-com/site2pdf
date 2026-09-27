package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"site2pdf/internal/pdfgen"
)

// maxResponseBytes は同期呼び出しの応答サイズ上限（6MB）。
// PDF・スクリーンショット(base64)と本文を合わせた応答全体で判定する。超える場合は将来 S3 に置いてURLを返す形にする。
const maxResponseBytes = 6 * 1024 * 1024

// 取得できる出力の種類（outputs に指定する値）。markdown, html は今後追加する。
const (
	outputJSON       = "json"
	outputPDF        = "pdf"
	outputScreenshot = "screenshot"
)

// request は Lambda に渡すイベントの形式。
type request struct {
	URL        string   `json:"url"`
	Outputs    []string `json:"outputs"`     // 取得するもの: json, pdf, screenshot（省略時は format から決める）
	Format     string   `json:"format"`      // 互換用: json（既定）, pdf, all。outputs と同時には指定できない
	TimeoutSec int      `json:"timeout_sec"` // ページ読み込みタイムアウト（0=環境変数の既定値）
	BlockAds   *bool    `json:"block_ads"`   // 広告を除くか（省略時=true）
}

// wants は outputs に name が含まれるかを返す。
func (r request) wants(name string) bool {
	for _, o := range r.Outputs {
		if o == name {
			return true
		}
	}
	return false
}

// screenshotJSON は応答に入れるスクリーンショット1枚。
type screenshotJSON struct {
	Y          int    `json:"y"`
	Height     int    `json:"height"`
	Bytes      int    `json:"bytes"`
	JPEGBase64 string `json:"jpeg_base64"`
}

// response は Lambda が返す結果の形式。
type response struct {
	URL                  string              `json:"url"`
	Outputs              []string            `json:"outputs"`
	Content              *pdfgen.PageContent `json:"content,omitempty"`
	PDFBase64            string              `json:"pdf_base64,omitempty"`
	PDFBytes             int                 `json:"pdf_bytes,omitempty"`
	Screenshots          []screenshotJSON    `json:"screenshots,omitempty"`
	ScreenshotsTruncated bool                `json:"screenshots_truncated,omitempty"` // 枚数上限・応答サイズ上限で下の方を省いた
	PageHeight           int                 `json:"page_height,omitempty"`
	LoadTimeout          bool                `json:"load_timeout,omitempty"` // load イベントを待ちきれず、読み込み途中の状態で取得した
	ElapsedMs            int64               `json:"elapsed_ms"`
	MemoryPeakMB         float64             `json:"memory_peak_mb,omitempty"` // 実行環境全体の最大メモリ（cgroup v2 で読める場合のみ）
}

// encodeResponse は応答をJSONにし、同期呼び出しの上限を超えていないか確かめる。
// 上限を超える場合はスクリーンショットを下から1枚ずつ省いて収める（screenshots_truncated を付ける）。
func encodeResponse(res *response) ([]byte, error) {
	for {
		body, err := json.Marshal(res)
		if err != nil {
			return nil, fmt.Errorf("応答のJSON化: %w", err)
		}
		if len(body) <= maxResponseBytes {
			return body, nil
		}
		if len(res.Screenshots) == 0 {
			return nil, fmt.Errorf("応答が大きすぎて同期応答に収まりません(%d bytes > %d, pdf %d bytes)", len(body), maxResponseBytes, res.PDFBytes)
		}
		res.Screenshots = res.Screenshots[:len(res.Screenshots)-1]
		res.ScreenshotsTruncated = true
	}
}

// config は環境変数から読み込む実行時設定。
type config struct {
	WorkDir           string
	ExtraArgs         []string
	StartupTimeoutSec int
	PageTimeoutSec    int
}

func loadConfig() config {
	return config{
		WorkDir:           envOr(pdfgen.WorkDirEnv, os.TempDir()),
		ExtraArgs:         strings.Fields(os.Getenv("SITE2PDF_CHROME_ARGS")),
		StartupTimeoutSec: envInt("SITE2PDF_STARTUP_TIMEOUT_SEC", 20),
		PageTimeoutSec:    envInt("SITE2PDF_TIMEOUT_SEC", 45),
	}
}

// parseRequest はイベントを検証し、フォーマットを正規化して返す。
func parseRequest(payload []byte) (request, error) {
	var req request
	if err := json.Unmarshal(payload, &req); err != nil {
		return req, fmt.Errorf("イベントのJSONが不正です: %w", err)
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return req, fmt.Errorf("url には http(s) の絶対URLを指定してください: %q", req.URL)
	}
	if len(req.Outputs) > 0 {
		if req.Format != "" {
			return req, fmt.Errorf("outputs と format は同時に指定できません")
		}
		seen := map[string]bool{}
		var outs []string
		for _, o := range req.Outputs {
			switch o {
			case outputJSON, outputPDF, outputScreenshot:
			default:
				return req, fmt.Errorf("outputs には json, pdf, screenshot を指定してください: %q", o)
			}
			if !seen[o] {
				seen[o] = true
				outs = append(outs, o)
			}
		}
		req.Outputs = outs
	} else {
		// 互換: format から outputs を決める（pdf/all は従来どおり本文も返す）
		switch req.Format {
		case "", string(pdfgen.FormatJSON):
			req.Outputs = []string{outputJSON}
		case string(pdfgen.FormatPDF), string(pdfgen.FormatAll):
			req.Outputs = []string{outputJSON, outputPDF}
		default:
			return req, fmt.Errorf("format は json, pdf, all のいずれかです: %q", req.Format)
		}
	}
	if req.TimeoutSec < 0 {
		return req, fmt.Errorf("timeout_sec は0以上で指定してください: %d", req.TimeoutSec)
	}
	return req, nil
}

// handle は1件のイベントを処理する。
func handle(ctx context.Context, payload []byte, cfg config, logger *slog.Logger) (*response, error) {
	chromeAlive, zombies := procStats()
	logger.Info("lambda.proc_stats", "chrome_alive", chromeAlive, "zombies", zombies)

	req, err := parseRequest(payload)
	if err != nil {
		return nil, err
	}

	// 呼び出しごとの作業ディレクトリ（実行環境が再利用されても前回の出力を残さない）
	dir, err := os.MkdirTemp(cfg.WorkDir, "site2pdf-req-*")
	if err != nil {
		return nil, fmt.Errorf("作業ディレクトリ作成: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	opts := pdfgen.DefaultGenerateOptions()
	opts.Format = pdfgen.FormatJSON
	opts.WorkDir = cfg.WorkDir
	opts.ExtraArgs = cfg.ExtraArgs
	opts.StartupTimeoutSec = cfg.StartupTimeoutSec
	opts.TimeoutSec = cfg.PageTimeoutSec
	if req.TimeoutSec > 0 {
		opts.TimeoutSec = req.TimeoutSec
	}
	if req.BlockAds != nil {
		opts.BlockAds = *req.BlockAds
	}
	outputPath := "" // JSON はファイルに書かず、戻り値の構造体をそのまま返す
	if req.wants(outputPDF) {
		opts.Format = pdfgen.FormatPDF
		outputPath = filepath.Join(dir, "out.pdf")
	}
	if req.wants(outputScreenshot) {
		so := pdfgen.DefaultScreenshotOptions()
		opts.Screenshot = &so
	}

	start := time.Now()
	content, err := pdfgen.ProcessPageHeadless(ctx, req.URL, outputPath, opts, logger)
	if err != nil {
		return nil, fmt.Errorf("ページ処理失敗(%s): %w", req.URL, err)
	}

	res := &response{URL: req.URL, Outputs: req.Outputs, LoadTimeout: content.LoadTimeout}
	if req.wants(outputJSON) {
		res.Content = content
	}

	if req.wants(outputPDF) {
		raw, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, fmt.Errorf("PDF読み込み: %w", err)
		}
		// base64 にした時点で上限を超えるものは、エンコードせずに早めに断る（最終判定は encodeResponse）
		if base64.StdEncoding.EncodedLen(len(raw)) > maxResponseBytes {
			return nil, fmt.Errorf("PDFが大きすぎて同期応答に収まりません(%d bytes)", len(raw))
		}
		res.PDFBytes = len(raw)
		res.PDFBase64 = base64.StdEncoding.EncodeToString(raw)
	}

	if shots := content.Screenshots; shots != nil {
		res.PageHeight = shots.PageHeight
		res.ScreenshotsTruncated = shots.Truncated
		for _, t := range shots.Tiles {
			res.Screenshots = append(res.Screenshots, screenshotJSON{
				Y: t.Y, Height: t.Height, Bytes: len(t.JPEG),
				JPEGBase64: base64.StdEncoding.EncodeToString(t.JPEG),
			})
		}
	}

	res.ElapsedMs = time.Since(start).Milliseconds()
	reapZombies()
	res.MemoryPeakMB = memoryPeakMB()
	logger.Info("lambda.done", "url", req.URL, "outputs", req.Outputs, "elapsed_ms", res.ElapsedMs,
		"pdf_bytes", res.PDFBytes, "screenshots", len(res.Screenshots), "memory_peak_mb", res.MemoryPeakMB)
	return res, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return def
	}
	return v
}
