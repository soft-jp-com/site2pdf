package pdfgen

import (
	"encoding/json"
	"testing"
)

func TestNavigationError(t *testing.T) {
	tests := []struct {
		name    string
		res     string
		wantErr bool
	}{
		{"成功", `{"frameId":"F1","loaderId":"L1"}`, false},
		{"DNS失敗", `{"frameId":"F1","errorText":"net::ERR_NAME_NOT_RESOLVED"}`, true},
		{"接続拒否", `{"frameId":"F1","errorText":"net::ERR_CONNECTION_REFUSED"}`, true},
		{"空のerrorText", `{"frameId":"F1","errorText":""}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := navigationError(json.RawMessage(tt.res), "https://example.invalid/")
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsChromeErrorPage(t *testing.T) {
	if !isChromeErrorPage("chrome-error://chromewebdata/") {
		t.Error("chrome-error:// はエラーページと判定されるはず")
	}
	if isChromeErrorPage("https://www.nta.go.jp/") {
		t.Error("通常のURLはエラーページではないはず")
	}
}
