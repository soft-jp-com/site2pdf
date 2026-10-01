# site2pdf 仕様書 (SPEC.md)

作成日: 2026-08-30

---

## 1. 概要と目的

`site2pdf` は、動的Webページ（SPA、JavaScript描画）や認証付きWebサイトから、**PDFファイル生成** および **テキスト/メタデータ抽出（JSON/Text）** を行うシステムです。

主に以下のユースケースを想定しています：
1. **証跡保管**: 企業の開示資料や規約、IRページ等のレイアウトをそのままPDFとしてローカル保存。
2. **ニュース・AIリサーチ基盤（`fnews` 等）の前処理エンジン**: 企業の最新リリースや有料メディア記事から、メイン本文（広告やナビゲーションを除去したクリーンテキスト）とメタ情報を構造化JSONとして抽出。

---

## 2. アーキテクチャと動作モード

```
                 ┌────────────────────────┐
                 │    site2pdf CLI (Go)   │
                 └───────────┬────────────┘
                             │
            ┌────────────────┴────────────────┐
            ▼                                 ▼
   【CDP モード】                    【ヘッドレス Chrome モード】
(起動中Chrome :9222等)              (新規ヘッドレスプロセス起動)
・生きたログインセッション          ・自動検出 / 指定パスのChrome
・クッキー/認証状態を完全維持       ・プロファイル指定可能
            │                                 │
            └────────────────┬────────────────┘
                             ▼
              Chrome DevTools Protocol (CDP)
             ├─ Page.navigate / loadEventFired
             ├─ Runtime.evaluate (本文/メタ抽出)
             ├─ Emulation (メディア展開)
             └─ Page.printToPDF (PDFバイナリ生成)
                             │
                             ▼
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
    [ PDF 生成 ]        [ JSON 抽出 ]       [ プレーンテキスト ]
 (.pdf ファイル保存)   (構造化データ出力)    (.txt ファイル/標準出力)
```

---

## 3. データ構造仕様

### 3.1 抽出コンテンツ（`PageContent`）のJSONスキーマ

```json
{
  "url": "https://example.com/news/123",
  "final_url": "https://example.com/news/123",
  "title": "ページタイトルまたは og:title",
  "site_name": "サイト名またはホスト名",
  "description": "meta description または og:description",
  "published_at": "2026-08-30T10:00:00Z",
  "article": "ナビゲーション・ヘッダー・フッター・広告等を除去したメイン本文テキスト",
  "text": "ページ全体の innerText",
  "fetched_at": "2026-08-30T12:00:00Z"
}
```

#### フィールド定義:
| フィールド | 型 | 説明 |
|---|---|---|
| `url` | string | 要求されたURL |
| `final_url` | string | リダイレクト後の最終URL (`window.location.href`) |
| `title` | string | `<title>` または `og:title` |
| `site_name` | string | `og:site_name` または ドメイン名 |
| `description` | string | `og:description` または `meta[name=description]` |
| `published_at` | string | `article:published_time` / `time[datetime]` 等から抽出した公開日時 |
| `article` | string | メイン本文（`<article>`, `<main>` を優先し、`<nav>`, `<header>`, `<footer>`, `<script>` 等を除外） |
| `text` | string | ページ全体のプレーンテキスト（`document.body.innerText`） |
| `fetched_at` | string (ISO8601) | 取得日時（UTC） |

---

## 4. 本文（Article）抽出アルゴリズム

`Runtime.evaluate` でページ内で以下の軽量DOMクレンジングを実行：
1. `document.body` のクローン（またはDOMツリー）から、ノイズ要素を除外：
   - タグ: `script`, `style`, `noscript`, `iframe`, `svg`, `nav`, `header`, `footer`, `aside`
   - クラス/ID: `.nav`, `.navbar`, `.header`, `.footer`, `.sidebar`, `.menu`, `.cookie-banner`, `.advertisement`, `.ad`
2. コンテナ優先探索：
   - `<article>` → `<main>` → `[role="main"]` → `.content` → `.post-content` → クローンルート
3. 取得した要素の `innerText` を正規化して `article` フィールドに格納。

---

## 5. 出力フォーマット仕様

| フォーマット (`-format`) | 説明 | 出力先 |
|---|---|---|
| `pdf` (デフォルト) | PDFバイナリを出力 | `-output <file/dir>` |
| `text` | ページのクリーンテキストを出力 | `-output <file/dir>` または 標準出力 |
| `json` | 上記 `PageContent` のJSON文字列を出力 | `-output <file/dir>` または 標準出力 |
| `all` | PDF, Text (.txt), JSON (.json) を同一ディレクトリに出力 | `-output <dir>` |

---

## 6. エラーハンドリングとタイムアウト

- **タイムアウト**: デフォルト 60秒（`-timeout` フラグで秒数変更可能）。
- **接続エラー**: リモートデバッグポートに接続できない場合、適切なヘルプメッセージを出力。
- **リソース管理**: 作成した CDP ターゲット（タブ）は、処理完了時またはエラー時に確実にクローズ (`Target.closeTarget`)。
