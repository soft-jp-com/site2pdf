package pdfgen

import (
	"strings"
	"testing"
)

func TestAdBlockPatterns(t *testing.T) {
	patterns := adBlockPatterns()
	if len(patterns) != len(adDomains)*2 {
		t.Fatalf("パターン数 = %d, want %d", len(patterns), len(adDomains)*2)
	}
	want := []string{"*://doubleclick.net/*", "*://*.doubleclick.net/*"}
	for _, w := range want {
		found := false
		for _, p := range patterns {
			if p == w {
				found = true
			}
		}
		if !found {
			t.Errorf("%q が含まれていない", w)
		}
	}
}

func TestHideAdSlotsCSSIsConservative(t *testing.T) {
	// 本文を消しかねない曖昧なセレクタを入れていないこと
	for _, bad := range []string{"[class*='ad']", "[id*='ad']", ".ad,", ".ads,", "aside"} {
		if strings.Contains(hideAdSlotsCSS, bad) {
			t.Errorf("曖昧なセレクタ %q が含まれている", bad)
		}
	}
	if !strings.Contains(hideAdSlotsCSS, "ins.adsbygoogle") {
		t.Error("ins.adsbygoogle が含まれていない")
	}
}

func TestJSString(t *testing.T) {
	got := jsString("a'b\\c\nd")
	if got != `'a\'b\\c\nd'` {
		t.Errorf("jsString = %s", got)
	}
}

func TestDefaultBlockAds(t *testing.T) {
	if !DefaultGenerateOptions().BlockAds {
		t.Error("既定では広告除去がオンのはず")
	}
}
