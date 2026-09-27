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
// PDF(base64)と本文を合わせた応答全体で判定する。超える場合は将来 S3 に置いてURLを返す形にする。
const maxResponseBytes = 6 * 1024 * 1024

// request は Lambda に渡すイベントの形式。
type request struct {
	URL        string `json:"url"`
	Format     string `json:"format"`      // json（既定）, pdf, all
	TimeoutSec int    `json:"timeout_sec"` // ページ読み込みタイムアウト（0=環境変数の既定値）
	BlockAds   *bool  `json:"block_ads"`   // 広告を除くか（省略時=true）
}

// response は Lambda が返す結果の形式。
type response struct {
	URL          string              `json:"url"`
	Format       string              `json:"format"`
	Content      *pdfgen.PageContent `json:"content"`
	PDFBase64    string              `json:"pdf_base64,omitempty"`
	PDFBytes     int                 `json:"pdf_bytes,omitempty"`
	ElapsedMs    int64               `json:"elapsed_ms"`
	MemoryPeakMB float64             `json:"memory_peak_mb,omitempty"` // 実行環境全体の最大メモリ（cgroup v2 で読める場合のみ）
}

// encodeResponse は応答をJSONにし、同期呼び出しの上限を超えていないか確かめる。
func encodeResponse(res *response) ([]byte, error) {
	body, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("応答のJSON化: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("応答が大きすぎて同期応答に収まりません(%d bytes > %d, pdf %d bytes)", len(body), maxResponseBytes, res.PDFBytes)
	}
	return body, nil
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
	switch req.Format {
	case "":
		req.Format = string(pdfgen.FormatJSON)
	case string(pdfgen.FormatJSON), string(pdfgen.FormatPDF), string(pdfgen.FormatAll):
	default:
		return req, fmt.Errorf("format は json, pdf, all のいずれかです: %q", req.Format)
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
	opts.Format = pdfgen.OutputFormat(req.Format)
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

	// all の場合は拡張子が付け替えられるので、PDFのパスは out.pdf になる
	outputPath := filepath.Join(dir, "out.pdf")
	if req.Format == string(pdfgen.FormatJSON) {
		outputPath = "" // JSON はファイルに書かず、戻り値の構造体をそのまま返す
	}

	start := time.Now()
	content, err := pdfgen.ProcessPageHeadless(ctx, req.URL, outputPath, opts, logger)
	if err != nil {
		return nil, fmt.Errorf("ページ処理失敗(%s): %w", req.URL, err)
	}

	res := &response{
		URL:     req.URL,
		Format:  req.Format,
		Content: content,
	}

	if req.Format != string(pdfgen.FormatJSON) {
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

	res.ElapsedMs = time.Since(start).Milliseconds()
	reapZombies()
	res.MemoryPeakMB = memoryPeakMB()
	logger.Info("lambda.done", "url", req.URL, "format", req.Format, "elapsed_ms", res.ElapsedMs,
		"pdf_bytes", res.PDFBytes, "memory_peak_mb", res.MemoryPeakMB)
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
