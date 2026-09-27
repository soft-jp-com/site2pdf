package main

import (
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantOutputs string
		wantErr     bool
	}{
		{"既定はjson", `{"url":"https://example.com/"}`, "json", false},
		{"format pdf は本文も返す", `{"url":"https://example.com/","format":"pdf"}`, "json,pdf", false},
		{"format all", `{"url":"http://example.com/a","format":"all"}`, "json,pdf", false},
		{"outputs 指定", `{"url":"https://example.com/","outputs":["screenshot","json"]}`, "screenshot,json", false},
		{"outputs の重複は1つに", `{"url":"https://example.com/","outputs":["pdf","pdf"]}`, "pdf", false},
		{"outputs に未対応の値", `{"url":"https://example.com/","outputs":["html"]}`, "", true},
		{"outputs と format の同時指定", `{"url":"https://example.com/","outputs":["json"],"format":"pdf"}`, "", true},
		{"textは未対応", `{"url":"https://example.com/","format":"text"}`, "", true},
		{"スキームなし", `{"url":"example.com"}`, "", true},
		{"file スキームは拒否", `{"url":"file:///etc/passwd"}`, "", true},
		{"url なし", `{}`, "", true},
		{"負のタイムアウト", `{"url":"https://example.com/","timeout_sec":-1}`, "", true},
		{"JSON不正", `{`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := parseRequest([]byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got := strings.Join(req.Outputs, ","); !tt.wantErr && got != tt.wantOutputs {
				t.Errorf("Outputs = %q, want %q", got, tt.wantOutputs)
			}
		})
	}
}

func TestEncodeResponseSizeLimit(t *testing.T) {
	small := &response{URL: "https://example.com/", Outputs: []string{"json"}}
	if _, err := encodeResponse(small); err != nil {
		t.Fatalf("小さい応答でエラー: %v", err)
	}

	big := &response{URL: "https://example.com/", Outputs: []string{"pdf"}, PDFBase64: strings.Repeat("A", maxResponseBytes)}
	if _, err := encodeResponse(big); err == nil {
		t.Fatal("上限を超える応答はエラーになるはず")
	}
}

func TestEncodeResponseDropsScreenshots(t *testing.T) {
	// 1枚2MBのスクリーンショット4枚（計8MB）は、下から省いて6MBに収める
	tile := screenshotJSON{JPEGBase64: strings.Repeat("A", 2*1024*1024)}
	res := &response{URL: "https://example.com/", Outputs: []string{"screenshot"},
		Screenshots: []screenshotJSON{tile, tile, tile, tile}}
	body, err := encodeResponse(res)
	if err != nil {
		t.Fatalf("エラーにならず枚数を減らして返すはず: %v", err)
	}
	if len(body) > maxResponseBytes {
		t.Errorf("応答 %d bytes が上限を超えている", len(body))
	}
	if len(res.Screenshots) != 2 || !res.ScreenshotsTruncated {
		t.Errorf("枚数 = %d, truncated = %v（2枚・true のはず）", len(res.Screenshots), res.ScreenshotsTruncated)
	}
}

func TestContextWithDeadline(t *testing.T) {
	ctx, cancel := contextWithDeadline("")
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Error("締め切りヘッダーが無いときは期限なしのはず")
	}

	ctx2, cancel2 := contextWithDeadline("4102444800000") // 2100-01-01
	defer cancel2()
	if _, ok := ctx2.Deadline(); !ok {
		t.Error("締め切りヘッダーがあるときは期限付きのはず")
	}
}
