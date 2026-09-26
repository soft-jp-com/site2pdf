// site2pdf Lambda: AWS Lambda（コンテナイメージ）上でヘッドレスChromeを動かし、
// 指定URLの構造化JSON・PDFを返す。
//
// aws-lambda-go の最新版は Go 1.26 以上を要求するため、依存を増やさず
// Lambda Runtime API（HTTP）を直接呼び出す最小限のランタイムループを実装している。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const runtimeAPIVersion = "2018-06-01"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	api := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	if api == "" {
		logger.Error("lambda.no_runtime_api", "msg", "AWS_LAMBDA_RUNTIME_API が未設定です（Lambda 環境または RIE で実行してください）")
		os.Exit(1)
	}

	if err := enableSubreaper(); err != nil {
		logger.Warn("lambda.subreaper_failed", "error", err.Error())
	}

	cfg := loadConfig()
	logger.Info("lambda.start", "chrome_path", os.Getenv("SITE2PDF_CHROME_PATH"), "extra_args", cfg.ExtraArgs)

	// 次のイベント待ちはロングポーリングになるためタイムアウトなし
	client := &http.Client{}
	base := "http://" + api + "/" + runtimeAPIVersion + "/runtime/invocation/"

	for {
		if err := invokeOnce(client, base, cfg, logger); err != nil {
			// Runtime API との通信自体の失敗は回復できないので終了する（Lambda が再起動する）
			logger.Error("lambda.runtime_error", "error", err.Error())
			os.Exit(1)
		}
	}
}

// invokeOnce は Runtime API から1件のイベントを受け取り、処理結果を返す。
func invokeOnce(client *http.Client, base string, cfg config, logger *slog.Logger) error {
	resp, err := client.Get(base + "next")
	if err != nil {
		return fmt.Errorf("get next invocation: %w", err)
	}
	payload, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read invocation payload: %w", err)
	}

	requestID := resp.Header.Get("Lambda-Runtime-Aws-Request-Id")
	ctx, cancel := contextWithDeadline(resp.Header.Get("Lambda-Runtime-Deadline-Ms"))
	defer cancel()

	out, herr := handle(ctx, payload, cfg, logger.With("request_id", requestID))
	// 失敗時も含め、Chrome の子孫プロセスをゾンビとして残さない
	reapZombies()
	var body []byte
	if herr == nil {
		body, herr = encodeResponse(out)
	}
	if herr != nil {
		logger.Error("lambda.invocation_error", "request_id", requestID, "error", herr.Error())
		ebody, _ := json.Marshal(map[string]string{
			"errorMessage": herr.Error(),
			"errorType":    "Site2PDFError",
		})
		return post(client, base+requestID+"/error", ebody)
	}
	return post(client, base+requestID+"/response", body)
}

// contextWithDeadline は Lambda の締め切り時刻から、応答を返す余裕(2秒)を残したコンテキストを作る。
func contextWithDeadline(deadlineMs string) (context.Context, context.CancelFunc) {
	ms, err := strconv.ParseInt(deadlineMs, 10, 64)
	if err != nil || ms <= 0 {
		return context.WithCancel(context.Background())
	}
	deadline := time.UnixMilli(ms).Add(-2 * time.Second)
	return context.WithDeadline(context.Background(), deadline)
}

func post(client *http.Client, url string, body []byte) error {
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("post %s: status %d", url, resp.StatusCode)
	}
	return nil
}
