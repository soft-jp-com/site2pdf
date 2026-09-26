package main

import (
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantFormat string
		wantErr    bool
	}{
		{"既定はjson", `{"url":"https://example.com/"}`, "json", false},
		{"pdf", `{"url":"https://example.com/","format":"pdf"}`, "pdf", false},
		{"all", `{"url":"http://example.com/a","format":"all"}`, "all", false},
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
			if !tt.wantErr && req.Format != tt.wantFormat {
				t.Errorf("Format = %q, want %q", req.Format, tt.wantFormat)
			}
		})
	}
}

func TestEncodeResponseSizeLimit(t *testing.T) {
	small := &response{URL: "https://example.com/", Format: "json"}
	if _, err := encodeResponse(small); err != nil {
		t.Fatalf("小さい応答でエラー: %v", err)
	}

	big := &response{URL: "https://example.com/", Format: "pdf", PDFBase64: strings.Repeat("A", maxResponseBytes)}
	if _, err := encodeResponse(big); err == nil {
		t.Fatal("上限を超える応答はエラーになるはず")
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
