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
}

// DefaultGenerateOptions はデフォルト設定を返す。
func DefaultGenerateOptions() GenerateOptions {
	return GenerateOptions{
		TimeoutSec:      60,
		PrintBackground: true,
		WindowWidth:     1920,
		WindowHeight:    1080,
		Format:          FormatPDF,
	}
}
