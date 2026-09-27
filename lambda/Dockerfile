# site2pdf Lambda コンテナイメージ
# ビルド（リポジトリのルートで実行）:
#   docker build -f lambda/Dockerfile -t site2pdf-lambda .
# 手元での動作確認は lambda/README.md を参照。

# ---- 1. Go のビルド ----
FROM public.ecr.aws/docker/library/golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/lambda

# ---- 2. Chrome for Testing の headless-shell を取得 ----
FROM public.ecr.aws/lambda/provided:al2023 AS chrome
# 版を固定して再現性を保つ。更新は https://googlechromelabs.github.io/chrome-for-testing/ を参照
ARG CHROME_VERSION=154.0.8037.57
RUN dnf install -y unzip && dnf clean all
ADD https://storage.googleapis.com/chrome-for-testing-public/${CHROME_VERSION}/linux64/chrome-headless-shell-linux64.zip /tmp/chs.zip
RUN unzip -q /tmp/chs.zip -d /opt && mv /opt/chrome-headless-shell-linux64 /opt/chrome && rm /tmp/chs.zip

# ---- 3. 実行イメージ ----
FROM public.ecr.aws/lambda/provided:al2023

# headless-shell が必要とする共有ライブラリと、日本語フォント（豆腐対策）
RUN dnf install -y \
        nss nspr atk at-spi2-atk cups-libs libdrm libxkbcommon \
        libXcomposite libXdamage libXfixes libXrandr libXext libX11 libxcb \
        mesa-libgbm pango cairo alsa-lib expat dbus-libs \
        fontconfig google-noto-sans-cjk-fonts \
    && dnf clean all \
    && rm -f /usr/share/fonts/google-noto-cjk/NotoSerifCJK* \
    && fc-cache -f
# ↑ Serif は使わないので削除してイメージを軽くする。
#   fc-cache でフォントキャッシュをイメージに焼き込む。これが無いと Lambda のコールドスタートごとに
#   フォント全体を走査して /tmp にキャッシュを作るため、Chrome の起動に20秒以上かかっていた（実機で確認）。

COPY --from=chrome /opt/chrome /opt/chrome

# 足りない共有ライブラリがあればビルドを失敗させる（ldd 自体が無い場合も失敗させるため、いったんファイルに書き出す）
RUN ldd /opt/chrome/chrome-headless-shell > /tmp/ldd.txt && ! grep -q "not found" /tmp/ldd.txt \
    && { echo /opt/chrome; \
         awk '$2 == "=>" && $3 ~ /^\// {print $3}' /tmp/ldd.txt; \
         echo /usr/lib/fontconfig/cache; \
         ls /usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc /usr/share/fonts/google-noto-cjk/NotoSansCJK-Bold.ttc 2>/dev/null; \
       } > /opt/prefetch.list \
    && rm /tmp/ldd.txt
# ↑ /opt/prefetch.list は初期化フェーズで先読みするファイルの一覧（Chrome 本体→共有ライブラリ→フォントの順）。
#   Lambda はイメージを必要な部分から読み込むため、コールドスタートで Chrome の起動に40秒かかっていた。

COPY --from=build /out/bootstrap ${LAMBDA_RUNTIME_DIR}/bootstrap

# Lambda で書き込めるのは /tmp だけなので、Chrome とフォントキャッシュの書き込み先を /tmp に向ける
ENV SITE2PDF_CHROME_PATH=/opt/chrome/chrome-headless-shell \
    SITE2PDF_WORK_DIR=/tmp \
    HOME=/tmp \
    XDG_CACHE_HOME=/tmp/.cache \
    XDG_CONFIG_HOME=/tmp/.config \
    FONTCONFIG_PATH=/etc/fonts \
    SITE2PDF_CHROME_ARGS="--no-zygote --disable-software-rasterizer" \
    SITE2PDF_STARTUP_TIMEOUT_SEC=20 \
    SITE2PDF_TIMEOUT_SEC=45 \
    SITE2PDF_PREFETCH_LIST=/opt/prefetch.list \
    SITE2PDF_PREFETCH_BUDGET_MS=8000

CMD ["bootstrap"]
