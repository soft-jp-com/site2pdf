package pdfgen

import "time"

// OutputFormat は出力フォーマットの種類を表す。
type OutputFormat string

const (
	FormatPDF  OutputFormat = "pdf"
	FormatText OutputFormat = "text"
	FormatJSON OutputFormat = "json"
	FormatAll  OutputFormat = "all"
)

// PageContent はページから抽出された構造化テキストコンテンツを表す。
// fnews などの他モジュールと直接連携可能なフィールド構成を持つ。
type PageContent struct {
	URL         string    `json:"url"`
	FinalURL    string    `json:"final_url"`
	Title       string    `json:"title"`
	SiteName    string    `json:"site_name,omitempty"`
	Description string    `json:"description,omitempty"`
	PublishedAt string    `json:"published_at,omitempty"`
	Article     string    `json:"article"` // ナビ・ヘッダー・フッター・広告等を除去したメイン本文
	Text        string    `json:"text"`    // ページ全体の innerText
	FetchedAt   time.Time `json:"fetched_at"`
	// LoadTimeout は load イベントが待ち時間内に来ず、読み込み途中の状態で抽出したことを示す
	LoadTimeout bool `json:"load_timeout,omitempty"`

	// Screenshots は GenerateOptions.Screenshot 指定時のみ設定される（JSON出力には含めない）
	Screenshots *Screenshots `json:"-"`
}

// GenerateOptions はPDF生成およびコンテンツ抽出の設定を保持する。
type GenerateOptions struct {
	// ChromePath はChromeの実行ファイルパス (空白=自動検出)
	ChromePath string
	// TimeoutSec はページ読み込みタイムアウト秒数
	TimeoutSec int
	// PrintBackground は背景色・背景画像を印刷するか
	PrintBackground bool
	// WindowWidth はブラウザウィンドウの幅(ピクセル)
	WindowWidth int
	// WindowHeight はブラウザウィンドウの高さ(ピクセル)
	WindowHeight int
	// UserDataDir はChromeのユーザーデータディレクトリパス
	UserDataDir string
	// ProfileDirectory はプロファイルディレクトリ名 (例: "Default")
	ProfileDirectory string
	// Format は出力フォーマット (pdf, text, json, all)
	Format OutputFormat
	// WorkDir は一時ユーザーデータディレクトリを作る親ディレクトリ
	// (空白=環境変数 SITE2PDF_WORK_DIR、それも空ならOS既定の一時ディレクトリ)
	WorkDir string
	// StartupTimeoutSec はヘッドレスChromeのデバッグポート待機秒数 (0=5秒)
	StartupTimeoutSec int
	// ExtraArgs はChrome起動時に追加する引数 (例: Lambda向けの --no-zygote)
	ExtraArgs []string
	// BlockAds は広告ドメインへの通信を止め、残った広告枠をCSSで隠すか
	BlockAds bool
	// LoadWaitSec は load イベントを待つ上限秒数 (0=15秒)。超えたらその時点の状態で処理を続ける
	LoadWaitSec int
	// Screenshot が非nilなら、Format とは別にスクリーンショットを撮り PageContent.Screenshots に入れる
	Screenshot *ScreenshotOptions
}

// DefaultGenerateOptions はデフォルト設定を返す。
func DefaultGenerateOptions() GenerateOptions {
	return GenerateOptions{
		TimeoutSec:      60,
		PrintBackground: true,
		WindowWidth:     1920,
		WindowHeight:    1080,
		Format:          FormatPDF,
		BlockAds:        true,
	}
}
