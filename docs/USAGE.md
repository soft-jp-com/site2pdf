# site2pdf 利用ガイド (USAGE.md)

---

## 1. 基本的な使い方

### 1.1 単一URLの取得

#### ① PDFとして保存（デフォルト）
```bash
site2pdf -url "https://example.com" -output "./output/example.pdf"
```

#### ② 構造化JSONとして取得（標準出力 または ファイル出力）
```bash
# 標準出力に出力（fnews やパイプライン連携用）
site2pdf -url "https://example.com" -json

# ファイルに保存
site2pdf -url "https://example.com" -json -output "./output/example.json"
```

#### ③ プレーンテキストとして取得
```bash
# 標準出力に出力
site2pdf -url "https://example.com" -text

# ファイルに保存
site2pdf -url "https://example.com" -text -output "./output/example.txt"
```

#### ④ PDF / Text / JSON をすべて一括保存
```bash
site2pdf -url "https://example.com" -format all -output "./output"
```

---

## 2. ログイン状態（認証済みセッション）の利用

有料メディアや会員制ページを取得する場合、起動中の Chrome に接続して生きたセッションを利用します。

### ステップ 1: Chrome をリモートデバッグ有効で起動
```bash
chrome.exe --remote-debugging-port=9222
```
ブラウザ上で対象サイトにログインします（ブラウザは開いたままにします）。

### ステップ 2: site2pdf を実行
```bash
# コマンドライン引数で指定
site2pdf -remote-port 9222 -url "https://example.com/protected-article" -json

# または site2pdf.json にポートを設定して実行
site2pdf -url "https://example.com/protected-article" -json
```

---

## 3. `sites.json` 一括保存モード

複数のURLをリスト化した `sites.json` を一括で処理します。

```json
{
  "service": "ExampleDocs",
  "documents": [
    { "no": 1, "name": "決算説明会資料", "url": "https://example.com/ir/q3", "localFile": "files/001_q3.pdf" },
    { "no": 2, "name": "適時開示", "url": "https://example.com/ir/news", "localFile": "files/002_news.pdf" }
  ]
}
```

```bash
# sites.json に基づいて一括PDF保存
site2pdf -sites "sites.json"

# 一括で JSON 形式で保存
site2pdf -sites "sites.json" -format json
```

---

## 4. サイトクロールモード（同一ドメイン巡回）

指定したURLから同一ドメイン内のリンクを辿って一括処理します。

```bash
site2pdf -crawl -url "https://example.com" -output "./output/crawled" -max-pages 20 -format json
```

---

## 5. コマンドライン引数一覧

| フラグ | デフォルト値 | 説明 |
|---|---|---|
| `-url` | `""` | 処理対象のURL（単一URLモードまたはクロール開始URL） |
| `-output` | `""` | 出力先ファイルパスまたはディレクトリ |
| `-format` | `"pdf"` | 出力形式 (`pdf`, `text`, `json`, `all`) |
| `-json` | `false` | `-format json` のショートハンド |
| `-text` | `false` | `-format text` のショートハンド |
| `-sites` | `""` | `sites.json` ファイルパス（一括モード） |
| `-crawl` | `false` | 同一ドメイン巡回クロールモード |
| `-max-pages` | `0` | 最大クロールページ数（0=無制限） |
| `-delay` | `500` | クロール時のリクエスト間隔（ミリ秒） |
| `-remote-port` | `0` | 起動中Chromeのリモートデバッグポート（>0 でCDP接続） |
| `-chrome` | `""` | Chrome実行ファイルのパス（省略時は自動検出） |
| `-timeout` | `60` | ページ読み込みタイムアウト（秒） |
| `-concurrency` | `3` | 一括処理時の並列実行数 |
| `-config` | `"site2pdf.json"` | 設定ファイルのパス |
| `-log-file` | `""` | 構造化ログの出力先ファイルパス（省略時はstderr） |
