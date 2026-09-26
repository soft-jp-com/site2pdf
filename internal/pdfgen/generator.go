// Feature: PDF-01 & TEXT-01
// ChromeヘッドレスモードでURLをロードし、PDF生成・テキスト抽出・構造化JSON抽出を行う。

package pdfgen

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// WorkDirEnv は一時ディレクトリの親ディレクトリを指定する環境変数名。
// Lambda では書き込み可能な /tmp を指定する。
const WorkDirEnv = "SITE2PDF_WORK_DIR"

// ProcessPageHeadless はバックグラウンドでChromeヘッドレスプロセスを起動し、
// CDP経由でページ処理（PDF生成・テキスト抽出・JSON抽出）を実行する。
func ProcessPageHeadless(ctx context.Context, pageURL, outputPath string, opts GenerateOptions, logger *slog.Logger) (*PageContent, error) {
	chromePath, err := resolveChromePath(opts.ChromePath)
	if err != nil {
		return nil, fmt.Errorf("resolve chrome path: %w", err)
	}

	// 空きポートを取得
	port, err := getFreePort()
	if err != nil {
		return nil, fmt.Errorf("get free port: %w", err)
	}

	// 一時ユーザーデータディレクトリを作成（プロファイル指定がない場合）
	userDataDir := opts.UserDataDir
	var tempDir string
	if userDataDir == "" {
		workDir := opts.WorkDir
		if workDir == "" {
			workDir = os.Getenv(WorkDirEnv)
		}
		tempDir, err = os.MkdirTemp(workDir, "site2pdf-headless-*")
		if err != nil {
			return nil, fmt.Errorf("create temp user data dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()
		userDataDir = tempDir
	} else {
		absDir, err := filepath.Abs(userDataDir)
		if err == nil {
			userDataDir = absDir
			cleanLockFiles(userDataDir)
		}
	}

	windowSize := "1920,1080"
	if opts.WindowWidth > 0 && opts.WindowHeight > 0 {
		windowSize = fmt.Sprintf("%d,%d", opts.WindowWidth, opts.WindowHeight)
	}

	// 高速化・軽量化のための最適化起動引数
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-breakpad",
		"--disable-component-extensions-with-background-pages",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-domain-reliability",
		"--disable-features=Translate,BackForwardCache,AcceptCHFrame,MediaRouter,OptimizationHints",
		"--disable-hang-monitor",
		"--disable-ipc-flooding-protection",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-renderer-backgrounding",
		"--disable-sync",
		"--force-color-profile=srgb",
		"--metrics-recording-only",
		"--no-first-run",
		"--no-default-browser-check",
		"--mute-audio",
		"--password-store=basic",
		"--use-mock-keychain",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		fmt.Sprintf("--window-size=%s", windowSize),
	}

	// テキスト/JSON抽出のみの場合は画像読み込みを無効化して通信量・レンダリング時間を劇的に短縮
	if opts.Format == FormatJSON || opts.Format == FormatText {
		args = append(args, "--blink-settings=imagesEnabled=false")
	}

	if opts.ProfileDirectory != "" {
		args = append(args, fmt.Sprintf("--profile-directory=%s", opts.ProfileDirectory))
	}

	args = append(args, opts.ExtraArgs...)
	args = append(args, "about:blank")

	logger.Info("chrome.launch_headless", "port", port, "chrome_path", chromePath)

	cmd := exec.CommandContext(ctx, chromePath, args...)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			// レンダラー等の子プロセスも残さないよう、プロセスグループごと終了する
			killProcessTree(cmd)
			_ = cmd.Wait()
		}
	}()

	// Chromeの起動・ポートリッスン完了を待機（既定5秒、高速ポーリング）
	startupTimeout := 5 * time.Second
	if opts.StartupTimeoutSec > 0 {
		startupTimeout = time.Duration(opts.StartupTimeoutSec) * time.Second
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, startupTimeout)
	ready := false
	for {
		if checkCtx.Err() != nil {
			break
		}
		if err := CheckRemoteDebugging(checkCtx, port); err == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	checkCancel()

	if !ready {
		return nil, fmt.Errorf("ヘッドレスChromeのリモートデバッグ待機タイムアウト(ポート%d)", port)
	}

	// CDP経由で処理実行
	return ProcessPageViaCDP(ctx, port, pageURL, outputPath, opts, logger)
}

// GeneratePDF は指定URLのページをChromeヘッドレスモードでPDFとして保存する（互換性ラッパー）。
func GeneratePDF(ctx context.Context, pageURL string, outputPath string, opts GenerateOptions, logger *slog.Logger) error {
	opts.Format = FormatPDF
	_, err := ProcessPageHeadless(ctx, pageURL, outputPath, opts, logger)
	return err
}

func getFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func resolveChromePath(chromePath string) (string, error) {
	if chromePath != "" {
		return chromePath, nil
	}
	return FindChrome()
}

func cleanLockFiles(userDataDir string) {
	locks := []string{"SingletonLock", "SingletonCookie", "SingletonSocket"}
	for _, l := range locks {
		p := filepath.Join(userDataDir, l)
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
		}
	}
}
