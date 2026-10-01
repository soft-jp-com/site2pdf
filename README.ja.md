# site2pdf

[English](README.md) | 日本語

Google Chrome を操作して、Web ページを **PDF**・**プレーンテキスト**・**構造化 JSON** で保存する Go 製のコマンドラインツールです。

## 特徴

- **出力形式**：レイアウトとスタイルを保った PDF、プレーンテキスト、構造化 JSON（タイトル、公開日時、メタ情報、ナビや広告を除いた本文、全文）、またはその 3 つすべて
- **Chrome の使い方を 2 通りから選べる**
  - **ヘッドレス**：PC の Chrome を自動で見つけ、裏で起動して使う
  - **起動中の Chrome に接続**：`--remote-debugging-port` 付きで起動した Chrome に接続し、ログインが必要なページもそのままのセッションで保存する
- **動的なサイト**：React・Vue・Next.js など JavaScript で描画されるページや、内側のスクロール領域も展開して保存する
- **広告のブロック**：広告配信先への通信を止め、広告枠を隠す（既定で有効。`-block-ads=false` で無効）
- **一括保存とクロール**：1 つの URL、`sites.json` に並べた URL、同じドメインのページの巡回に対応

## 必要なもの

- Google Chrome（または Chromium）
- Go 1.22 以上（ビルドする場合のみ）

主な対象は Windows です。macOS と Linux でもビルドできます。

## ビルド

```bash
go build -o dist/site2pdf ./cmd/site2pdf
```

Windows では `build.bat` を実行すると `dist\site2pdf.exe` ができます。

## 使い方

```bash
# PDF で保存
site2pdf -url "https://example.com" -output "./output/example.pdf"

# 構造化 JSON を標準出力に出す
site2pdf -url "https://example.com" -json

# PDF・テキスト・JSON をまとめてフォルダに保存
site2pdf -url "https://example.com" -format all -output "./output"

# --remote-debugging-port=9222 で起動した Chrome のセッションを使う
site2pdf -remote-port 9222 -url "https://example.com/members-only" -json

# sites.json に並べた文書をまとめて保存
site2pdf -sites "sites.json"

# 同じドメインを最大 20 ページ巡回
site2pdf -crawl -url "https://example.com" -output "./output/crawled" -max-pages 20
```

すべてのオプションは `site2pdf -h` で表示できます。詳しい説明は次の文書にあります。

- [docs/USAGE.md](docs/USAGE.md)：オプション、設定ファイル、使用例
- [docs/SPEC.md](docs/SPEC.md)：構成、データモデル、JSON の形式

## テスト

```bash
go test ./...
```

## ライセンス

[Apache License 2.0](LICENSE)
