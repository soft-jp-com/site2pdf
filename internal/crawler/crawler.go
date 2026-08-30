// Feature: CRAWL-01
// 指定ドメイン内のすべてのページURLをBFSで収集する。

package crawler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// CrawlSite は指定したURLからサイト内のすべてのページ情報を収集して返す。
// 同一ホスト名のページのみを対象とし、外部ドメインは除外する。
// 失敗条件: startURL が不正な場合、または ctx がキャンセルされた場合。
// 副作用: 対象サイトへのHTTPリクエストを発行する。
func CrawlSite(ctx context.Context, startURL string, opts CrawlOptions, logger *slog.Logger) ([]PageInfo, error) {
	parsedStart, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("start url parse: %w", err)
	}

	baseDomain := parsedStart.Hostname()
	if baseDomain == "" {
		return nil, fmt.Errorf("start url has no hostname: %s", startURL)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	visited := make(map[string]bool)
	queue := []string{normalizeURL(parsedStart)}
	var results []PageInfo

	for len(queue) > 0 {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		if opts.MaxPages > 0 && len(results) >= opts.MaxPages {
			break
		}

		currentURL := queue[0]
		queue = queue[1:]

		if visited[currentURL] {
			continue
		}
		visited[currentURL] = true

		if opts.ProgressFunc != nil {
			opts.ProgressFunc(len(queue)+len(results)+1, len(results), currentURL)
		}

		logger.Info("crawl.fetch",
			"url", currentURL,
			"visited_count", len(visited),
			"queue_count", len(queue),
		)

		title, links, fetchErr := fetchPageData(ctx, client, currentURL, opts.UserAgent)
		if fetchErr != nil {
			logger.Warn("crawl.fetch_failed",
				"url", currentURL,
				"error", fetchErr.Error(),
			)
			continue
		}

		results = append(results, PageInfo{URL: currentURL, Title: title})

		// 同一ドメイン内の未訪問リンクをキューに追加する
		for _, link := range links {
			parsed, parseErr := url.Parse(link)
			if parseErr != nil {
				continue
			}
			if !isSameDomain(parsed, baseDomain) {
				continue
			}
			normalized := normalizeURL(parsed)
			if !visited[normalized] {
				queue = append(queue, normalized)
			}
		}

		// レート制限: サーバー負荷を抑えるために待機する
		if opts.DelayMs > 0 {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			case <-time.After(time.Duration(opts.DelayMs) * time.Millisecond):
			}
		}
	}

	return results, nil
}

// fetchPageData は指定URLのHTMLを取得し、ページタイトルとリンクURLを返す。
// HTML以外のコンテンツタイプの場合はタイトル空・リンク空で返す。
// 副作用: HTTPリクエストを発行する。
func fetchPageData(ctx context.Context, client *http.Client, pageURL string, userAgent string) (title string, links []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		return "", nil, nil
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse base url: %w", err)
	}

	return extractPageData(resp.Body, base)
}

// extractPageData はHTMLをパースし、ページタイトルと全リンクURLを一度に返す。
// <title> タグと <a href> タグを同一トラバーサルで収集することでHTMLの重複読み込みを避ける。
// フラグメント・javascript:・mailto:・tel: リンクは除外する。
// 副作用: r から読み込む。
func extractPageData(r io.Reader, base *url.URL) (title string, links []string, err error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", nil, fmt.Errorf("html parse: %w", err)
	}

	seen := make(map[string]bool)

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				// <title> タグのテキストノードを取得する
				if title == "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					title = strings.TrimSpace(n.FirstChild.Data)
				}
			case "a":
				for _, attr := range n.Attr {
					if attr.Key != "href" {
						continue
					}
					href := strings.TrimSpace(attr.Val)
					if href == "" || strings.HasPrefix(href, "#") ||
						strings.HasPrefix(href, "javascript:") ||
						strings.HasPrefix(href, "mailto:") ||
						strings.HasPrefix(href, "tel:") {
						continue
					}
					ref, parseErr := url.Parse(href)
					if parseErr != nil {
						continue
					}
					abs := base.ResolveReference(ref)
					abs.Fragment = ""
					absStr := abs.String()
					if !seen[absStr] {
						seen[absStr] = true
						links = append(links, absStr)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(doc)
	return title, links, nil
}

// normalizeURL はURLを正規化した文字列を返す。
// フラグメントを除去し、末尾スラッシュは保持する。
func normalizeURL(u *url.URL) string {
	normalized := *u
	normalized.Fragment = ""
	return normalized.String()
}

// isSameDomain はURLのホスト名がbaseDomainと一致するか確認する。
// http/https スキームのみ対象とする。
func isSameDomain(u *url.URL, baseDomain string) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Hostname() == baseDomain
}
