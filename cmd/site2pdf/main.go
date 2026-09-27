// site2pdf: Web ページを PDF・プレーンテキスト・構造化JSON に変換・抽出する CLI ツール。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"site2pdf/internal/crawler"
	"site2pdf/internal/pdfgen"
)

// appConfig は設定ファイル(site2pdf.json)の内容を表す。
type appConfig struct {
	RemoteDebuggingPort int    `json:"remote_debugging_port"`
	GoogleAccount       string `json:"google_account"`
	ChromeUserDataDir   string `json:"chrome_user_data_dir"`
}

// siteDocument は sites.json の documents 配列の1要素を表す。
type siteDocument struct {
	No        int    `json:"no"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalFile string `json:"localFile"`
}

// sitesConfig は sites.json 全体の内容を表す。
type sitesConfig struct {
	Service   string         `json:"service"`
	Updated   string         `json:"updated"`
	Documents []siteDocument `json:"documents"`
}

func loadSitesConfig(path string) (*sitesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sites.json 読み込み(%s): %w", path, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var s sitesConfig
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("sites.json 解析(%s): %w", path, err)
	}
	return &s, nil
}

func loadConfig(path string) (*appConfig, error) {
	var cfg appConfig
	if _, err := os.Stat(path); err != nil {
		return &cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("設定ファイル読み込み失敗(%s): %w", path, err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイル解析失敗(%s): %w", path, err)
	}
	return &cfg, nil
}

func findConfigPath(name string) string {
	if _, err := os.Stat(name); err == nil {
		return name
	}
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return name
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flagURL := flag.String("url", "", "処理対象URL / クロール開始URL")
	flagOutput := flag.String("output", "", "出力先ファイルまたはディレクトリ (省略時かつjson/text指定時は標準出力)")
	flagFormat := flag.String("format", "pdf", "出力フォーマット (pdf, text, json, all)")
	flagJSON := flag.Bool("json", false, "-format json のショートハンド")
	flagText := flag.Bool("text", false, "-format text のショートハンド")

	flagSitesFile := flag.String("sites", "", "処理対象の sites.json のパス (指定時または単一URLなしで存在時に一括実行)")
	flagCrawl := flag.Bool("crawl", false, "同一ドメイン内を巡回するクロールモード")
	flagMaxPages := flag.Int("max-pages", 0, "最大クロールページ数 (0=無制限)")
	flagDelayMs := flag.Int("delay", 500, "クロール時のリクエスト待機時間(ms)")

	flagConcurrency := flag.Int("concurrency", 3, "並列処理数")
	flagChrome := flag.String("chrome", "", "Chrome実行ファイルのパス (省略時=自動検出)")
	flagTimeoutSec := flag.Int("timeout", 60, "ページ読み込みタイムアウト(秒)")
	flagPrintBg := flag.Bool("print-background", true, "背景色・背景画像を印刷するか")
	flagBlockAds := flag.Bool("block-ads", true, "広告ドメインへの通信を止め、広告枠を隠す (-block-ads=false で無効)")
	flagLogFile := flag.String("log-file", "", "構造化ログの出力先ファイルパス (省略時=stderr)")
	flagUserDataDir := flag.String("user-data-dir", "", "Chromeのユーザーデータディレクトリのパス")
	flagProfileDir := flag.String("profile-directory", "", "使用するプロファイルディレクトリ名 (例: Default, Profile 1)")
	flagUseActiveSession := flag.Bool("use-active-session", true, "アクティブなブラウザのセッションを一時フォルダにコピーして自動で利用する")
	flagConfig := flag.String("config", "site2pdf.json", "設定ファイルのパス")
	flagRemotePort := flag.Int("remote-port", 0, "起動中Chromeのリモートデバッグポート (>0 でCDP直接接続)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "使い方 (Usage) of site2pdf:\n\n")
		fmt.Fprintf(os.Stderr, "1. 単一URLモード (WebページをPDF/Text/JSONに変換・抽出):\n")
		fmt.Fprintf(os.Stderr, "   site2pdf -url <URL> [-output <ファイル/ディレクトリ>] [-format pdf|text|json|all]\n")
		fmt.Fprintf(os.Stderr, "   site2pdf -url <URL> -json               (構造化JSONを標準出力に出力)\n")
		fmt.Fprintf(os.Stderr, "   site2pdf -url <URL> -text               (プレーンテキストを標準出力に出力)\n\n")
		fmt.Fprintf(os.Stderr, "2. sites.json 一括保存モード:\n")
		fmt.Fprintf(os.Stderr, "   site2pdf -sites <sites.json> [-format pdf|text|json|all]\n\n")
		fmt.Fprintf(os.Stderr, "3. ドメイン巡回クロールモード:\n")
		fmt.Fprintf(os.Stderr, "   site2pdf -crawl -url <URL> -output <ディレクトリ> [-format pdf|text|json|all]\n\n")
		fmt.Fprintf(os.Stderr, "4. ログイン状態のセッションを利用する場合:\n")
		fmt.Fprintf(os.Stderr, "   1) chrome.exe --remote-debugging-port=9222 でChromeを起動してログイン\n")
		fmt.Fprintf(os.Stderr, "   2) site2pdf -remote-port 9222 -url <URL> -json\n\n")
		fmt.Fprintf(os.Stderr, "オプション一覧:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// 位置引数のフォールバック処理
	args := flag.Args()
	targetURL := *flagURL
	outputFile := *flagOutput
	if targetURL == "" && len(args) > 0 {
		targetURL = args[0]
		if len(args) > 1 && outputFile == "" {
			outputFile = args[1]
		}
	}

	// フォーマット決定
	selectedFormat := pdfgen.OutputFormat(strings.ToLower(*flagFormat))
	if *flagJSON {
		selectedFormat = pdfgen.FormatJSON
	} else if *flagText {
		selectedFormat = pdfgen.FormatText
	}
	if selectedFormat == "" {
		selectedFormat = pdfgen.FormatPDF
	}

	cfg, err := loadConfig(findConfigPath(*flagConfig))
	if err != nil {
		return err
	}

	remotePort := *flagRemotePort
	portFromFlag := true
	if remotePort == 0 {
		remotePort = cfg.RemoteDebuggingPort
		portFromFlag = false
	}

	if remotePort > 0 {
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
		connErr := pdfgen.CheckRemoteDebugging(checkCtx, remotePort)
		checkCancel()
		if connErr != nil {
			if portFromFlag {
				return fmt.Errorf("リモートデバッグ(ポート%d)に接続できません: %w", remotePort, connErr)
			}
			// 設定ファイル由来の場合はヘッドレスモードにフォールバック
			fmt.Fprintf(os.Stderr, "[情報] リモートデバッグ(ポート%d)が未起動のため、ヘッドレスChromeで実行します。\n", remotePort)
			remotePort = 0
		} else {
			*flagUseActiveSession = false
			fmt.Fprintf(os.Stderr, "[情報] 起動中のChrome(ポート%d)に接続し、そのログインセッションで処理します。\n", remotePort)
		}
	}

	var tempUserDataDir string
	var tempCleanup func() = func() {}
	defer func() { tempCleanup() }()

	resolvedProfileDir := *flagProfileDir

	if *flagUseActiveSession && remotePort == 0 {
		resolvedChrome := *flagChrome
		if resolvedChrome == "" {
			var err error
			resolvedChrome, err = pdfgen.FindChrome()
			if err != nil {
				return fmt.Errorf("Chrome自動検出失敗: %w", err)
			}
		}

		tempDir, chosenProfile, cleanup, err := setupActiveSession(resolvedChrome, *flagProfileDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[警告] アクティブブラウザのセッション取得に失敗しました。セッションなしで続行します: %v\n", err)
		} else {
			tempUserDataDir = tempDir
			tempCleanup = cleanup
			resolvedProfileDir = chosenProfile
		}
	}

	logger, logCloser, err := initLogger(*flagLogFile)
	if err != nil {
		return fmt.Errorf("ロガー初期化: %w", err)
	}
	defer logCloser()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	userDataDir := *flagUserDataDir
	if tempUserDataDir != "" {
		userDataDir = tempUserDataDir
	}

	pdfOpts := pdfgen.GenerateOptions{
		ChromePath:       *flagChrome,
		TimeoutSec:       *flagTimeoutSec,
		PrintBackground:  *flagPrintBg,
		WindowWidth:      1920,
		WindowHeight:     1080,
		UserDataDir:      userDataDir,
		ProfileDirectory: resolvedProfileDir,
		Format:           selectedFormat,
		BlockAds:         *flagBlockAds,
	}

	// ページ処理の共通実行関数
	processSingle := func(ctx context.Context, u, out string, opts pdfgen.GenerateOptions) (*pdfgen.PageContent, error) {
		if remotePort > 0 {
			return pdfgen.ProcessPageViaCDP(ctx, remotePort, u, out, opts, logger)
		}
		return pdfgen.ProcessPageHeadless(ctx, u, out, opts, logger)
	}

	// -------------------------------------------------------------
	// モード1: 単一URLモード
	// -------------------------------------------------------------
	if targetURL != "" && !*flagCrawl {
		// 出力先が未指定かつ (json または text) の場合は標準出力に出す
		stdoutMode := (outputFile == "") && (selectedFormat == pdfgen.FormatJSON || selectedFormat == pdfgen.FormatText)
		if outputFile == "" && !stdoutMode {
			// デフォルトはドメインやタイトルからファイル名を推測
			outputFile = "./output"
		}

		if !stdoutMode {
			fmt.Fprintf(os.Stderr, "[処理開始] %s (format: %s) -> %s\n", targetURL, selectedFormat, outputFile)
		}

		content, err := processSingle(ctx, targetURL, outputFile, pdfOpts)
		if err != nil {
			return fmt.Errorf("処理失敗: %w", err)
		}

		if stdoutMode && content != nil {
			if selectedFormat == pdfgen.FormatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(content)
			} else if selectedFormat == pdfgen.FormatText {
				text := content.Article
				if text == "" {
					text = content.Text
				}
				fmt.Println(text)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[完了] %s (タイトル: %s)\n", targetURL, content.Title)
		}
		return nil
	}

	// -------------------------------------------------------------
	// モード2: クロールモード
	// -------------------------------------------------------------
	if targetURL != "" && *flagCrawl {
		if outputFile == "" {
			outputFile = "./output"
		}
		crawlOpts := crawler.CrawlOptions{
			MaxPages:  *flagMaxPages,
			DelayMs:   *flagDelayMs,
			UserAgent: "site2pdf/1.0",
			ProgressFunc: func(discovered, visited int, currentURL string) {
				fmt.Fprintf(os.Stderr, "\r[クロール] 処理済: %d / 発見: %d  %s          ",
					visited, discovered, truncateString(currentURL, 60))
			},
		}

		fmt.Fprintf(os.Stderr, "クロール開始: %s\n", targetURL)
		crawlStart := time.Now()
		pages, err := crawler.CrawlSite(ctx, targetURL, crawlOpts, logger)
		crawlLatencyMs := time.Since(crawlStart).Milliseconds()
		fmt.Fprintln(os.Stderr)
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("クロール失敗: %w", err)
		}
		if len(pages) == 0 {
			return fmt.Errorf("変換対象のページが見つかりませんでした")
		}
		fmt.Fprintf(os.Stderr, "クロール完了: %d ページ発見 (%dms)\n\n", len(pages), crawlLatencyMs)

		parsedURL, err := url.Parse(targetURL)
		if err != nil {
			return fmt.Errorf("URL解析: %w", err)
		}
		domainDir := filepath.Join(outputFile, sanitizePathComponent(parsedURL.Hostname()))

		type crawlTask struct {
			url        string
			outputPath string
			title      string
		}

		stemCount := make(map[string]int)
		var tasks []crawlTask
		ext := "." + string(selectedFormat)
		if selectedFormat == pdfgen.FormatAll {
			ext = ""
		}

		for _, p := range pages {
			stem := titleToFileStem(p.Title, p.URL)
			count := stemCount[stem]
			stemCount[stem]++

			var filename string
			if count == 0 {
				filename = stem + ext
			} else {
				filename = fmt.Sprintf("%s_%d%s", stem, count+1, ext)
			}

			tasks = append(tasks, crawlTask{
				url:        p.URL,
				outputPath: filepath.Join(domainDir, filename),
				title:      p.Title,
			})
		}

		fmt.Fprintf(os.Stderr, "処理開始: %d ページ -> %s (format: %s)\n", len(tasks), domainDir, selectedFormat)
		var successCount, failCount atomic.Int32
		sem := make(chan struct{}, *flagConcurrency)
		var wg sync.WaitGroup
		batchStart := time.Now()

		for i, task := range tasks {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}

			go func(idx int, t crawlTask) {
				defer wg.Done()
				defer func() { <-sem }()

				content, genErr := processSingle(ctx, t.url, t.outputPath, pdfOpts)
				if genErr != nil {
					failCount.Add(1)
					fmt.Fprintf(os.Stderr, "[%d/%d] ✗ %s\n  -> %v\n", idx+1, len(tasks), t.url, genErr)
				} else {
					successCount.Add(1)
					title := t.title
					if content != nil && content.Title != "" {
						title = content.Title
					}
					fmt.Fprintf(os.Stderr, "[%d/%d] ✓ %s\n  -> %s\n", idx+1, len(tasks), title, filepath.Base(t.outputPath))
				}
			}(i, task)
		}
		wg.Wait()

		fmt.Fprintf(os.Stderr, "\n処理完了: 成功=%d 失敗=%d (所要時間: %dms)\n出力先: %s\n",
			successCount.Load(), failCount.Load(),
			time.Since(batchStart).Milliseconds(),
			domainDir,
		)
		return nil
	}

	// -------------------------------------------------------------
	// モード3: sites.json 一括処理モード
	// -------------------------------------------------------------
	sitesPathToUse := *flagSitesFile
	if sitesPathToUse == "" {
		// カレントまたは実行ファイルの sites.json を探す
		found := findConfigPath("sites.json")
		if _, err := os.Stat(found); err == nil {
			sitesPathToUse = found
		}
	}

	if sitesPathToUse == "" {
		flag.Usage()
		return fmt.Errorf("URL または sites.json を指定してください")
	}

	sitesPath := findConfigPath(sitesPathToUse)
	sites, err := loadSitesConfig(sitesPath)
	if err != nil {
		return err
	}
	if len(sites.Documents) == 0 {
		return fmt.Errorf("sites.json (%s) に処理対象のドキュメントがありません", sitesPath)
	}

	baseDir := filepath.Dir(sitesPath)
	type docTask struct {
		url  string
		out  string
		name string
	}
	var tasks []docTask
	for _, d := range sites.Documents {
		if strings.TrimSpace(d.URL) == "" || strings.TrimSpace(d.LocalFile) == "" {
			fmt.Fprintf(os.Stderr, "[スキップ] no=%d name=%q (url または localFile が未指定)\n", d.No, d.Name)
			continue
		}
		out := filepath.FromSlash(d.LocalFile)
		if !filepath.IsAbs(out) {
			out = filepath.Join(baseDir, out)
		}
		// format に応じて拡張子を調整
		if selectedFormat == pdfgen.FormatJSON && !strings.HasSuffix(strings.ToLower(out), ".json") {
			out = replaceExt(out, ".json")
		} else if selectedFormat == pdfgen.FormatText && !strings.HasSuffix(strings.ToLower(out), ".txt") {
			out = replaceExt(out, ".txt")
		} else if selectedFormat == pdfgen.FormatAll {
			out = stripExt(out)
		}

		tasks = append(tasks, docTask{url: d.URL, out: out, name: d.Name})
	}

	if len(tasks) == 0 {
		return fmt.Errorf("有効な(url と localFile の両方がある)ドキュメントがありません")
	}

	fmt.Fprintf(os.Stderr, "一括処理開始: %d ドキュメント (service=%s, format=%s) -> %s\n", len(tasks), sites.Service, selectedFormat, baseDir)
	var successCount, failCount atomic.Int32
	sem := make(chan struct{}, *flagConcurrency)
	var wg sync.WaitGroup
	batchStart := time.Now()

	for i, task := range tasks {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, t docTask) {
			defer wg.Done()
			defer func() { <-sem }()

			content, genErr := processSingle(ctx, t.url, t.out, pdfOpts)
			if genErr != nil {
				failCount.Add(1)
				fmt.Fprintf(os.Stderr, "[%d/%d] ✗ %s\n  -> %v\n", idx+1, len(tasks), t.url, genErr)
			} else {
				successCount.Add(1)
				name := t.name
				if name == "" && content != nil {
					name = content.Title
				}
				fmt.Fprintf(os.Stderr, "[%d/%d] ✓ %s\n  -> %s\n", idx+1, len(tasks), name, filepath.Base(t.out))
			}
		}(i, task)
	}
	wg.Wait()

	fmt.Fprintf(os.Stderr, "\n処理完了: 成功=%d 失敗=%d (所要時間: %dms)\n出力先: %s\n",
		successCount.Load(), failCount.Load(),
		time.Since(batchStart).Milliseconds(),
		baseDir,
	)
	return nil
}

func titleToFileStem(title string, pageURL string) string {
	if strings.TrimSpace(title) == "" {
		return urlToFileStem(pageURL)
	}
	stem := sanitizePathComponent(strings.TrimSpace(title))
	if strings.Trim(stem, " .") == "" {
		return urlToFileStem(pageURL)
	}
	runes := []rune(stem)
	if len(runes) > 200 {
		stem = string(runes[:200])
	}
	return stem
}

func urlToFileStem(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil || u.Path == "" || u.Path == "/" {
		return "index"
	}
	path := strings.TrimSuffix(u.Path, "/")
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return sanitizePathComponent(parts[i])
		}
	}
	return "index"
}

func initLogger(logFilePath string) (*slog.Logger, func(), error) {
	var w io.Writer = os.Stderr
	closer := func() {}
	if logFilePath != "" {
		f, err := os.Create(logFilePath)
		if err != nil {
			return nil, closer, fmt.Errorf("ログファイル作成: %w", err)
		}
		w = f
		closer = func() { f.Close() }
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler), closer, nil
}

func sanitizePathComponent(name string) string {
	replacer := strings.NewReplacer(
		`\`, "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		`"`, "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func replaceExt(path, newExt string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + newExt
	}
	return strings.TrimSuffix(path, ext) + newExt
}

func stripExt(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path
	}
	return strings.TrimSuffix(path, ext)
}

func findActiveUserDataDir(chromePath string) string {
	lowerPath := strings.ToLower(chromePath)
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	if strings.Contains(lowerPath, "msedge") {
		return filepath.Join(localAppData, `Microsoft\Edge\User Data`)
	}
	return filepath.Join(localAppData, `Google\Chrome\User Data`)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func setupActiveSession(chromePath string, profileDir string) (string, string, func(), error) {
	srcUserDataDir := findActiveUserDataDir(chromePath)
	if srcUserDataDir == "" {
		return "", "", func() {}, fmt.Errorf("LOCALAPPDATA 環境変数が見つかりません")
	}

	if _, err := os.Stat(srcUserDataDir); err != nil {
		return "", "", func() {}, fmt.Errorf("アクティブなブラウザのデータディレクトリが見つかりません: %s", srcUserDataDir)
	}

	if profileDir == "" {
		profileDir = detectBestProfile(srcUserDataDir)
	}

	tempDir, err := os.MkdirTemp("", "site2pdf-session-*")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("一時ディレクトリ作成失敗: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	filesToCopy := []struct {
		srcRel  string
		destRel string
	}{
		{srcRel: "Local State", destRel: "Local State"},
		{srcRel: filepath.Join(profileDir, "Network", "Cookies"), destRel: filepath.Join(profileDir, "Network", "Cookies")},
		{srcRel: filepath.Join(profileDir, "Cookies"), destRel: filepath.Join(profileDir, "Cookies")},
		{srcRel: filepath.Join(profileDir, "Preferences"), destRel: filepath.Join(profileDir, "Preferences")},
	}

	copiedAny := false
	for _, f := range filesToCopy {
		srcPath := filepath.Join(srcUserDataDir, f.srcRel)
		destPath := filepath.Join(tempDir, f.destRel)

		if _, err := os.Stat(srcPath); err != nil {
			continue
		}
		if err := copyFile(srcPath, destPath); err == nil {
			copiedAny = true
		}
	}

	if !copiedAny {
		cleanup()
		return "", "", func() {}, fmt.Errorf("セッション情報のコピーに失敗しました")
	}

	return tempDir, profileDir, cleanup, nil
}

func detectBestProfile(userDataDir string) string {
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return "Default"
	}
	best := ""
	var bestSize int64 = -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name != "Default" && !strings.HasPrefix(name, "Profile ") {
			continue
		}
		var totalSize int64
		for _, rel := range []string{filepath.Join(name, "Network", "Cookies"), filepath.Join(name, "Cookies")} {
			if fi, err := os.Stat(filepath.Join(userDataDir, rel)); err == nil {
				totalSize += fi.Size()
			}
		}
		if totalSize > bestSize {
			bestSize = totalSize
			best = name
		}
	}
	if best == "" {
		return "Default"
	}
	return best
}
