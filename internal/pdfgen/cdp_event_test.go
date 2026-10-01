package pdfgen

import (
	"encoding/json"
	"testing"
)

func TestKeepEvent(t *testing.T) {
	tests := []struct {
		method, params string
		want           bool
	}{
		{"Page.loadEventFired", `{}`, true},
		{"Runtime.consoleAPICalled", `{}`, true},
		{"Network.requestWillBeSent", `{}`, false},
		{"Network.dataReceived", `{}`, false},
		{"Network.loadingFinished", `{}`, false},
		{"Network.responseReceived", `{"type":"Image"}`, false},
		{"Network.responseReceived", `{"type":"Document"}`, true},
	}
	for _, tt := range tests {
		got := keepEvent(cdpMessage{Method: tt.method, Params: json.RawMessage(tt.params)})
		if got != tt.want {
			t.Errorf("keepEvent(%s %s) = %v, want %v", tt.method, tt.params, got, tt.want)
		}
	}
}
