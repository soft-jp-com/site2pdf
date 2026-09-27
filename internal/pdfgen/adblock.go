package pdfgen

import (
	"context"
	"strings"
)

// adDomains は通信を止める広告配信・計測ドメイン。
// 本文の表示に使われるドメインを誤って入れないよう、広告専業のものに限る。
var adDomains = []string{
	// Google
	"doubleclick.net",
	"googlesyndication.com",
	"googleadservices.com",
	"adservice.google.com",
	"adservice.google.co.jp",
	"imasdk.googleapis.com",
	// 海外の主要アドネットワーク
	"amazon-adsystem.com",
	"adnxs.com",
	"criteo.com",
	"criteo.net",
	"taboola.com",
	"outbrain.com",
	"rubiconproject.com",
	"pubmatic.com",
	"openx.net",
	"casalemedia.com",
	"smartadserver.com",
	"teads.tv",
	"adsrvr.org",
	// 国内のアドネットワーク
	"i-mobile.co.jp",
	"microad.jp",
	"microad.net",
	"impact-ad.jp",
	"ad-stir.com",
	"adingo.jp",
	"fluct.jp",
	"gmossp-sp.jp",
	"yads.yahoo.co.jp",
	"yads.c.yimg.jp",
	"logly.co.jp",
	"popin.cc",
	"uncn.jp",
	"socdm.com",
	"ladsp.com",
	"genieesspv.jp",
	"gsspat.jp",
	"adlantis.jp",
	"nend.net",
}

// adBlockPatterns は Network.setBlockedURLs に渡すURLパターンを返す。
func adBlockPatterns() []string {
	patterns := make([]string, 0, len(adDomains)*2)
	for _, d := range adDomains {
		patterns = append(patterns, "*://"+d+"/*", "*://*."+d+"/*")
	}
	return patterns
}

// hideAdSlotsCSS は通信を止めても残る広告枠を隠すCSS。
// 本文を誤って消さないよう、広告タグが必ず付けるIDやクラスだけに絞る
// （"ad" を含むクラス名のような曖昧な指定はしない）。
var hideAdSlotsCSS = strings.Join([]string{
	"ins.adsbygoogle",
	"[id^='google_ads_iframe']",
	"[id^='div-gpt-ad']",
	"iframe[src*='doubleclick.net']",
	"iframe[src*='googlesyndication.com']",
	"iframe[src*='amazon-adsystem.com']",
	".trc_related_container", // Taboola
	".OUTBRAIN",              // Outbrain
}, ",\n") + " { display: none !important; }"

// enableAdBlock は広告ドメインへの通信を止める。ページ遷移の前に呼ぶ。
func enableAdBlock(ctx context.Context, cli *cdpClient, sid string) error {
	if _, err := cli.send(ctx, "Network.enable", nil, sid); err != nil {
		return err
	}
	patterns := adBlockPatterns()
	urlPatterns := make([]map[string]any, 0, len(patterns))
	for _, p := range patterns {
		urlPatterns = append(urlPatterns, map[string]any{"urlPattern": p, "block": true})
	}
	// Chrome の版により引数名が異なる（旧: urls、新: urlPatterns）ため両方渡す
	_, err := cli.send(ctx, "Network.setBlockedURLs", map[string]any{
		"urls":        patterns,
		"urlPatterns": urlPatterns,
	}, sid)
	return err
}

// hideAdSlotsJS は広告枠を隠す style 要素をページに追加するJS。
func hideAdSlotsJS() string {
	return `(function(){var s=document.createElement('style');s.setAttribute('data-site2pdf','hide-ads');s.textContent=` +
		jsString(hideAdSlotsCSS) + `;(document.head||document.documentElement).appendChild(s);})()`
}

// jsString はGoの文字列をJSの文字列リテラルにする。
func jsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)
	return "'" + r.Replace(s) + "'"
}
