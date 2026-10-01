package crawler

// PageInfo はクロールで取得したページの情報を保持する。
type PageInfo struct {
	// URL はページのURL
	URL string
	// Title はページの <title> タグの内容。取得できない場合は空文字。
	Title string
}

// CrawlOptions はクロールの設定を保持する。
type CrawlOptions struct {
	// MaxPages は最大クロールページ数 (0=無制限)
	MaxPages int
	// DelayMs はリクエスト間の待機時間(ミリ秒)
	DelayMs int
	// UserAgent はHTTPリクエストのUser-Agent
	UserAgent string
	// ProgressFunc は進捗報告コールバック (nilの場合は無効)
	// 引数: 発見済み件数, 処理済み件数, 現在のURL
	ProgressFunc func(discovered, visited int, currentURL string)
}

// DefaultCrawlOptions はデフォルト設定を返す。
func DefaultCrawlOptions() CrawlOptions {
	return CrawlOptions{
		MaxPages:  0,
		DelayMs:   500,
		UserAgent: "site2pdf/1.0",
	}
}
