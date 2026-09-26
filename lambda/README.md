# site2pdf Lambda（試作）

AWS Lambda（コンテナイメージ）上でヘッドレス Chrome を動かし、指定 URL の構造化 JSON と PDF を返す。
AWS へのデプロイ手順はまだ無い。ここでは手元の Docker と Runtime Interface Emulator（RIE、ベースイメージに同梱）で確認する。

## 構成

| 項目 | 内容 |
|---|---|
| ハンドラー | `cmd/lambda`（Lambda Runtime API を直接呼ぶ。aws-lambda-go の最新版が Go 1.26 以上を要求するため依存を増やしていない） |
| ベースイメージ | `public.ecr.aws/lambda/provided:al2023`（x86_64） |
| Chrome | Chrome for Testing の `chrome-headless-shell`（版は Dockerfile の `CHROME_VERSION`） |
| 日本語フォント | `google-noto-sans-cjk-fonts`（AL2023 では `google-noto-cjk-fonts` が入る） |

## イベントと応答

```json
{"url": "https://www.nta.go.jp/", "format": "json", "timeout_sec": 30}
```

- `format`: `json`（既定）/ `pdf` / `all`（`pdf` と `all` は JSON に加えて PDF を返す）
- 応答: `content`（タイトル・本文など）, `pdf_base64`, `pdf_bytes`, `elapsed_ms`, `memory_peak_mb`（cgroup v2 の memory.peak が読める場合のみ）
- 応答全体（PDF の base64 と本文の JSON を合わせたもの）が 6MB（同期呼び出しの上限）を超えるとエラーを返す。大きい PDF は S3 に置く形が今後の課題。

## 環境変数

| 変数 | 既定値（イメージ内） | 用途 |
|---|---|---|
| `SITE2PDF_CHROME_PATH` | `/opt/chrome/chrome-headless-shell` | Chrome の実行ファイル |
| `SITE2PDF_WORK_DIR` | `/tmp` | Chrome の一時プロファイル等の置き場所 |
| `SITE2PDF_CHROME_ARGS` | `--no-zygote --disable-software-rasterizer` | 追加の起動引数（空白区切り） |
| `SITE2PDF_STARTUP_TIMEOUT_SEC` | `20` | Chrome 起動待ち |
| `SITE2PDF_TIMEOUT_SEC` | `45` | ページ読み込みタイムアウト |

`SITE2PDF_CHROME_PATH` と `SITE2PDF_WORK_DIR` は CLI 版（`cmd/site2pdf`）でも有効。

## 手元での確認（PowerShell）

リポジトリのルートで実行する。

```powershell
docker build -f lambda/Dockerfile -t site2pdf-lambda .

# Lambda に近い制約（書き込みは /tmp だけ、メモリ 2GB）で起動
docker run -d --name s2p-rie -p 9000:8080 --memory=2g --read-only --tmpfs /tmp:rw,exec,size=512m site2pdf-lambda

Invoke-RestMethod -Method Post -Uri http://localhost:9000/2015-03-31/functions/function/invocations `
  -ContentType 'application/json' -Body '{"url":"https://www.nta.go.jp/","format":"json"}'

# 後片付け
docker rm -f s2p-rie
```

PDF を保存する場合:

```powershell
$r = Invoke-RestMethod -Method Post -Uri http://localhost:9000/2015-03-31/functions/function/invocations `
  -ContentType 'application/json' -Body '{"url":"https://www.nta.go.jp/","format":"pdf"}'
[IO.File]::WriteAllBytes("$env:TEMP\nta.pdf", [Convert]::FromBase64String($r.pdf_base64))
```

## 注意点

- Chrome の子プロセスは Lambda の PID 1 に引き取られると回収されずゾンビとして残るため、
  ハンドラーを subreaper にして呼び出しごとに回収している（直前に終了した分が次回まで1つ程度残ることはある）。
- RIE は Lambda のサンドボックス制約を完全には再現しない。実機での確認は未実施。
- イメージは約 1.1GB（CJK フォントの Serif も含まれるため）。Sans の日本語だけに絞れば小さくできる。
