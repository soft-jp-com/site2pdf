// Feature: PDF-02 & TEXT-01
// 起動中のChromeにChrome DevTools Protocol(CDP)で接続し、その生きたログイン
// セッションを使ってページをPDF化・テキスト抽出・構造化JSON抽出する。
// クッキーのコピー/復号が不要なため、App-Bound暗号化(v20)やセッション限定クッキーでも
// ログイン状態のまま取得できる。

package pdfgen

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultLoadWait は load イベントを待つ上限。超えたらその時点の状態で処理を続ける。
const defaultLoadWait = 15 * time.Second

// settleAfterLoad はload完了後、JS描画の安定化を待つ時間。
const settleAfterLoad = 2 * time.Second

// extractContentJS はページ上でDOMからメタデータとメイン本文、全体テキストを抽出するスクリプト。
const extractContentJS = `(function() {
  function getMeta(prop, name) {
    if (prop) {
      var el = document.querySelector('meta[property="' + prop + '"]');
      if (el && el.content) return el.content.trim();
    }
    if (name) {
      var el = document.querySelector('meta[name="' + name + '"]');
      if (el && el.content) return el.content.trim();
    }
    return '';
  }

  var title = document.title || '';
  var ogTitle = getMeta('og:title');
  if (!title && ogTitle) title = ogTitle;

  var siteName = getMeta('og:site_name') || getMeta('', 'application-name') || window.location.hostname;
  var description = getMeta('og:description') || getMeta('', 'description') || '';

  var publishedAt = getMeta('article:published_time') || getMeta('', 'date') || getMeta('', 'pubdate') || '';
  if (!publishedAt) {
    var timeEl = document.querySelector('time[datetime]');
    if (timeEl) publishedAt = timeEl.getAttribute('datetime') || timeEl.innerText.trim();
  }

  var fullText = document.body ? (document.body.innerText || '') : '';

  // メイン本文抽出: 不要な要素を除去
  var clone = document.body ? document.body.cloneNode(true) : null;
  var articleText = '';
  if (clone) {
    var removeSelectors = [
      'script', 'style', 'noscript', 'iframe', 'svg',
      'nav', 'header', 'footer', 'aside',
      '.nav', '.navbar', '.header', '.footer', '.sidebar', '.menu',
      '.cookie-banner', '.advertisement', '.ad', '#cookie-notice'
    ];
    removeSelectors.forEach(function(sel) {
      var nodes = clone.querySelectorAll(sel);
      for (var i = 0; i < nodes.length; i++) {
        if (nodes[i].parentNode) {
          nodes[i].parentNode.removeChild(nodes[i]);
        }
      }
    });

    var mainEl = clone.querySelector('article') || 
                 clone.querySelector('main') || 
                 clone.querySelector('[role="main"]') || 
                 clone.querySelector('.content') || 
                 clone.querySelector('.post-content') || 
                 clone.querySelector('.entry-content') || 
                 clone;
    articleText = mainEl ? (mainEl.innerText || '').trim() : '';
  }
  if (!articleText) {
    articleText = fullText.trim();
  }

  return {
    url: window.location.href,
    final_url: window.location.href,
    title: title.trim(),
    site_name: siteName.trim(),
    description: description.trim(),
    published_at: publishedAt.trim(),
    article: articleText,
    text: fullText.trim()
  };
})()`

// unclipScrollContainersJS はページ内のスクロール領域や高さ制限を解除し、
// 全コンテンツを文書フローに展開する。
const unclipScrollContainersJS = `(function(){
  try{
    var st=document.createElement('style');
    st.textContent='html,body{height:auto!important;max-height:none!important;overflow:visible!important;}';
    document.documentElement.appendChild(st);
    var all=document.querySelectorAll('*');
    for(var i=0;i<all.length;i++){
      var el=all[i];
      var cs=window.getComputedStyle(el);
      var ov=cs.overflow+' '+cs.overflowX+' '+cs.overflowY;
      if(ov.indexOf('auto')>=0||ov.indexOf('scroll')>=0){
        el.style.overflow='visible';
        el.style.maxHeight='none';
        el.style.height='auto';
      }
    }
  }catch(e){}
  return 0;
})()`

func waitCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("CDPエラー %d: %s", e.Code, e.Message) }

type cdpMessage struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type wsConn struct {
	conn   net.Conn
	r      *bufio.Reader
	writeM sync.Mutex
}

func dialWebSocket(wsURL string) (*wsConn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		return nil, err
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	r := bufio.NewReader(conn)
	statusLine, err := r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("WebSocketハンドシェイク失敗: %s", strings.TrimSpace(statusLine))
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &wsConn{conn: conn, r: r}, nil
}

func (w *wsConn) writeText(payload []byte) error {
	w.writeM.Lock()
	defer w.writeM.Unlock()

	var header []byte
	header = append(header, 0x80|0x1) // FIN + text
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(0x80|n))
	case n < 65536:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)

	masked := make([]byte, n)
	for i := range masked {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *wsConn) readMessage() ([]byte, error) {
	var buf []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0f
		masked := h[1]&0x80 != 0
		length := int(h[1] & 0x7f)

		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			length = int(binary.BigEndian.Uint64(ext[:]))
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case 0x8: // Close
			return nil, io.EOF
		case 0x9: // Ping
			_ = w.writeControl(0xA, payload)
			continue
		case 0xA: // Pong
			continue
		case 0x0, 0x1, 0x2: // Continuation, Text, Binary
			buf = append(buf, payload...)
			if fin {
				return buf, nil
			}
		}
	}
}

func (w *wsConn) writeControl(opcode byte, payload []byte) error {
	w.writeM.Lock()
	defer w.writeM.Unlock()
	header := []byte{0x80 | opcode, byte(len(payload))}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.conn.Write(payload)
		return err
	}
	return nil
}

func (w *wsConn) close() { _ = w.conn.Close() }

type cdpClient struct {
	ws      *wsConn
	mu      sync.Mutex
	nextID  int
	pending map[int]chan cdpMessage
	events  chan cdpMessage
	closed  chan struct{}
	readErr error
}

func newCDPClient(wsURL string) (*cdpClient, error) {
	ws, err := dialWebSocket(wsURL)
	if err != nil {
		return nil, err
	}
	c := &cdpClient{
		ws:      ws,
		pending: make(map[int]chan cdpMessage),
		events:  make(chan cdpMessage, 256),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *cdpClient) readLoop() {
	defer close(c.closed)
	for {
		data, err := c.ws.readMessage()
		if err != nil {
			c.readErr = err
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Method != "" && keepEvent(msg) {
			select {
			case c.events <- msg:
			default:
			}
		}
	}
}

// keepEvent はイベントをキューに入れるかを返す。
// キューが満杯だとイベントは捨てられるため、Network ドメインを有効にしたときに
// リクエストごとに大量に届くイベントで Page.loadEventFired が押し出されないよう、使わないものは入れない。
// Network.responseReceived はメインのドキュメント（HTTPステータスの取得に使う予定）だけ通す。
func keepEvent(msg cdpMessage) bool {
	if !strings.HasPrefix(msg.Method, "Network.") {
		return true
	}
	if msg.Method != "Network.responseReceived" {
		return false
	}
	var p struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(msg.Params, &p) == nil && p.Type == "Document"
}

func (c *cdpClient) send(ctx context.Context, method string, params map[string]any, sessionID string) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if sessionID != "" {
		req["sessionId"] = sessionID
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := c.ws.writeText(b); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, fmt.Errorf("CDP接続が切断されました: %v", c.readErr)
	case msg := <-ch:
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	}
}

func (c *cdpClient) waitEvent(ctx context.Context, method, sessionID string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closed:
			return fmt.Errorf("CDP接続が切断されました: %v", c.readErr)
		case ev := <-c.events:
			if ev.Method == method && (sessionID == "" || ev.SessionID == sessionID) {
				return nil
			}
		}
	}
}

func (c *cdpClient) eval(ctx context.Context, sessionID, expr string) (json.RawMessage, error) {
	res, err := c.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
	}, sessionID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	return r.Result.Value, nil
}

func (c *cdpClient) close() { c.ws.close() }

func browserWSURL(ctx context.Context, port int) (string, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("/json/version 解析失敗: %w", err)
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("webSocketDebuggerUrl が取得できませんでした")
	}
	return v.WebSocketDebuggerURL, nil
}

// CheckRemoteDebugging はリモートデバッグに接続できるか確認する。
func CheckRemoteDebugging(ctx context.Context, port int) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := browserWSURL(cctx, port)
	return err
}

// ProcessPageViaCDP は起動中のChrome(リモートデバッグ有効)に接続し、
// 対象URLを開いて指定されたフォーマット(PDF/Text/JSON/All)に従って処理を行う。
// 戻り値として抽出された PageContent を返す。
func ProcessPageViaCDP(ctx context.Context, port int, pageURL, outputPath string, opts GenerateOptions, logger *slog.Logger) (*PageContent, error) {
	timeout := time.Duration(opts.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	wsURL, err := browserWSURL(cctx, port)
	if err != nil {
		return nil, fmt.Errorf("リモートデバッグに接続できません(ポート%d): %w", port, err)
	}

	cli, err := newCDPClient(wsURL)
	if err != nil {
		return nil, fmt.Errorf("CDP接続失敗: %w", err)
	}
	defer cli.close()

	start := time.Now()
	logger.Info("cdp.process_start", "url", pageURL, "port", port, "format", string(opts.Format))

	// 新規タブ(ターゲット)を作成
	res, err := cli.send(cctx, "Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget: %w", err)
	}
	var tgt struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &tgt); err != nil || tgt.TargetID == "" {
		return nil, fmt.Errorf("targetId の取得に失敗しました")
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_, _ = cli.send(closeCtx, "Target.closeTarget", map[string]any{"targetId": tgt.TargetID}, "")
	}()

	// タブにアタッチ
	res, err = cli.send(cctx, "Target.attachToTarget", map[string]any{"targetId": tgt.TargetID, "flatten": true}, "")
	if err != nil {
		return nil, fmt.Errorf("Target.attachToTarget: %w", err)
	}
	var att struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &att); err != nil || att.SessionID == "" {
		return nil, fmt.Errorf("sessionId の取得に失敗しました")
	}
	sid := att.SessionID

	if _, err := cli.send(cctx, "Page.enable", nil, sid); err != nil {
		return nil, fmt.Errorf("Page.enable: %w", err)
	}
	if _, err := cli.send(cctx, "Runtime.enable", nil, sid); err != nil {
		return nil, fmt.Errorf("Runtime.enable: %w", err)
	}
	if opts.BlockAds {
		// 失敗しても本来の処理は続ける（広告が残るだけ）
		if err := enableAdBlock(cctx, cli, sid); err != nil {
			logger.Warn("cdp.adblock_failed", "url", pageURL, "error", err.Error())
		}
	}
	if opts.Screenshot != nil {
		if err := setScreenshotViewport(cctx, cli, sid, *opts.Screenshot); err != nil {
			return nil, fmt.Errorf("Emulation.setDeviceMetricsOverride: %w", err)
		}
	}
	navRes, err := cli.send(cctx, "Page.navigate", map[string]any{"url": pageURL}, sid)
	if err != nil {
		return nil, fmt.Errorf("Page.navigate: %w", err)
	}
	if err := navigationError(navRes, pageURL); err != nil {
		return nil, err
	}

	format := opts.Format
	if format == "" {
		format = FormatPDF
	}
	needPDF := (format == FormatPDF || format == FormatAll)
	needText := (format == FormatText || format == FormatAll)
	needJSON := (format == FormatJSON || format == FormatAll)

	// load完了待機。広告などが読み込みを終えず load イベントが来ないページもあるため、
	// 一定時間で打ち切ってその時点の状態で処理を続ける（LoadTimeout を立てる）
	loadWait := defaultLoadWait
	if opts.LoadWaitSec > 0 {
		loadWait = time.Duration(opts.LoadWaitSec) * time.Second
	}
	loadTimedOut := false
	lctx, lcancel := context.WithTimeout(cctx, loadWait)
	err = cli.waitEvent(lctx, "Page.loadEventFired", sid)
	lcancel()
	if err != nil {
		if cctx.Err() != nil || lctx.Err() != context.DeadlineExceeded {
			return nil, fmt.Errorf("ページ読み込み待機: %w", err)
		}
		loadTimedOut = true
		logger.Warn("cdp.load_timeout", "url", pageURL, "wait", loadWait.String())
	}

	if opts.BlockAds {
		// 通信を止めても残る広告枠（空の枠や自社配信の広告）を隠す
		_, _ = cli.eval(cctx, sid, hideAdSlotsJS())
	}

	if needPDF || opts.Screenshot != nil {
		// PDF・スクリーンショット生成時はCSS展開・スクロール展開・安定化待機を行う
		if err := waitCtx(cctx, settleAfterLoad); err != nil {
			return nil, err
		}
		_, _ = cli.send(cctx, "Emulation.setEmulatedMedia", map[string]any{"media": "screen"}, sid)
		_, _ = cli.eval(cctx, sid, "window.scrollTo(0, document.body ? document.body.scrollHeight : 0)")
		_ = waitCtx(cctx, 600*time.Millisecond)
		_, _ = cli.eval(cctx, sid, "window.scrollTo(0, 0)")
	} else {
		// テキスト/JSON抽出のみの場合は最小限の待機(150ms)で即座に抽出
		_ = waitCtx(cctx, 150*time.Millisecond)
	}

	// 1. テキスト/メタデータ抽出
	rawContent, err := cli.eval(cctx, sid, extractContentJS)
	if err != nil {
		return nil, fmt.Errorf("コンテンツ抽出JS実行失敗: %w", err)
	}

	var content PageContent
	if err := json.Unmarshal(rawContent, &content); err != nil {
		return nil, fmt.Errorf("抽出コンテンツ解析失敗: %w", err)
	}
	content.FetchedAt = time.Now().UTC()
	content.LoadTimeout = loadTimedOut
	if opts.Screenshot != nil && !isChromeErrorPage(content.FinalURL) {
		shots, err := captureScreenshots(cctx, cli, sid, *opts.Screenshot)
		if err != nil {
			return nil, err
		}
		content.Screenshots = shots
	}
	// 読み込み途中で失敗した場合もエラーページになるので、成功として返さない
	if isChromeErrorPage(content.FinalURL) {
		return nil, fmt.Errorf("ページを開けません(%s): エラーページが表示されました", pageURL)
	}

	if outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return nil, fmt.Errorf("出力先ディレクトリ作成: %w", err)
		}
	}

	// PDF 生成
	if needPDF && outputPath != "" {
		// スクロール領域の高さ制限を外して全コンテンツを印刷する。
		// 親の高さが固定されたサイトでは中身がはみ出して重なる（朝日新聞の右列など）ため、
		// スクリーンショットには使わず、撮り終えた後・印刷の直前にだけ行う。
		_, _ = cli.eval(cctx, sid, unclipScrollContainersJS)
		_ = waitCtx(cctx, 300*time.Millisecond)

		pdfPath := outputPath
		if format == FormatAll {
			pdfPath = replaceOrAppendExt(outputPath, ".pdf")
		}
		absPDF, err := filepath.Abs(pdfPath)
		if err != nil {
			return nil, fmt.Errorf("resolve pdf path: %w", err)
		}

		res, err := cli.send(cctx, "Page.printToPDF", map[string]any{
			"printBackground":   opts.PrintBackground,
			"preferCSSPageSize": false,
		}, sid)
		if err != nil {
			return nil, fmt.Errorf("Page.printToPDF: %w", err)
		}
		var out struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(res, &out); err != nil {
			return nil, fmt.Errorf("printToPDF 結果解析: %w", err)
		}
		raw, err := base64.StdEncoding.DecodeString(out.Data)
		if err != nil {
			return nil, fmt.Errorf("PDFデコード: %w", err)
		}
		if err := os.WriteFile(absPDF, raw, 0644); err != nil {
			return nil, fmt.Errorf("PDF書き込み: %w", err)
		}
		logger.Info("pdf.saved", "path", absPDF)
	}

	// プレーンテキスト出力
	if needText && outputPath != "" {
		txtPath := outputPath
		if format == FormatAll || !strings.HasSuffix(strings.ToLower(txtPath), ".txt") {
			txtPath = replaceOrAppendExt(outputPath, ".txt")
		}
		absTxt, err := filepath.Abs(txtPath)
		if err != nil {
			return nil, fmt.Errorf("resolve txt path: %w", err)
		}
		outText := content.Article
		if outText == "" {
			outText = content.Text
		}
		if err := os.WriteFile(absTxt, []byte(outText), 0644); err != nil {
			return nil, fmt.Errorf("テキスト書き込み: %w", err)
		}
		logger.Info("text.saved", "path", absTxt)
	}

	// JSON 出力
	if needJSON && outputPath != "" {
		jsonPath := outputPath
		if format == FormatAll || !strings.HasSuffix(strings.ToLower(jsonPath), ".json") {
			jsonPath = replaceOrAppendExt(outputPath, ".json")
		}
		absJSON, err := filepath.Abs(jsonPath)
		if err != nil {
			return nil, fmt.Errorf("resolve json path: %w", err)
		}
		jsonData, err := json.MarshalIndent(content, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("JSONシリアライズ: %w", err)
		}
		if err := os.WriteFile(absJSON, jsonData, 0644); err != nil {
			return nil, fmt.Errorf("JSON書き込み: %w", err)
		}
		logger.Info("json.saved", "path", absJSON)
	}

	logger.Info("cdp.process_done", "url", pageURL, "latency_ms", time.Since(start).Milliseconds())
	return &content, nil
}

// navigationError は Page.navigate の結果に errorText（DNS失敗・接続拒否など）があればエラーを返す。
// これを見ないと chrome-error ページを正常なページとして抽出してしまう。
func navigationError(navRes json.RawMessage, pageURL string) error {
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(navRes, &nav); err == nil && nav.ErrorText != "" {
		return fmt.Errorf("ページを開けません(%s): %s", pageURL, nav.ErrorText)
	}
	return nil
}

// isChromeErrorPage は最終URLがChromeのエラーページかどうかを返す。
func isChromeErrorPage(finalURL string) bool {
	return strings.HasPrefix(finalURL, "chrome-error://")
}

// GeneratePDFViaCDPは後方互換性のためのラッパー関数。
func GeneratePDFViaCDP(ctx context.Context, port int, pageURL, outputPath string, opts GenerateOptions, logger *slog.Logger) error {
	opts.Format = FormatPDF
	_, err := ProcessPageViaCDP(ctx, port, pageURL, outputPath, opts, logger)
	return err
}

// ExtractContentViaCDP はコンテンツ抽出のみを行うラッパー関数。
func ExtractContentViaCDP(ctx context.Context, port int, pageURL string, opts GenerateOptions, logger *slog.Logger) (*PageContent, error) {
	opts.Format = FormatJSON
	return ProcessPageViaCDP(ctx, port, pageURL, "", opts, logger)
}

func replaceOrAppendExt(path, newExt string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + newExt
	}
	return strings.TrimSuffix(path, ext) + newExt
}
