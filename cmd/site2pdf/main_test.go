package main

import (
	"testing"
)

// titleToFileStem のテスト
func TestTitleToFileStem(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		pageURL  string
		expected string
	}{
		{
			name:     "通常タイトル",
			title:    "はじめに",
			pageURL:  "https://example.com/intro",
			expected: "はじめに",
		},
		{
			name:     "Windows禁止文字を含むタイトル",
			title:    "ガイド: 入門 / 基礎",
			pageURL:  "https://example.com/guide",
			expected: "ガイド_ 入門 _ 基礎",
		},
		{
			name:     "タイトルが空のときURLフォールバック",
			title:    "",
			pageURL:  "https://example.com/about",
			expected: "about",
		},
		{
			name:     "タイトルが空でルートURLのとき index",
			title:    "",
			pageURL:  "https://example.com/",
			expected: "index",
		},
		{
			name:     "空白のみのタイトルはURLフォールバック",
			title:    "   ",
			pageURL:  "https://example.com/docs",
			expected: "docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := titleToFileStem(tt.title, tt.pageURL)
			if got != tt.expected {
				t.Errorf("titleToFileStem(%q, %q) = %q, want %q", tt.title, tt.pageURL, got, tt.expected)
			}
		})
	}
}

func TestPathHelpers(t *testing.T) {
	if got := replaceExt("output/test.pdf", ".json"); got != "output/test.json" {
		t.Errorf("replaceExt got %q, want %q", got, "output/test.json")
	}
	if got := replaceExt("output/test", ".json"); got != "output/test.json" {
		t.Errorf("replaceExt got %q, want %q", got, "output/test.json")
	}
	if got := stripExt("output/test.pdf"); got != "output/test" {
		t.Errorf("stripExt got %q, want %q", got, "output/test")
	}
	if got := stripExt("output/test"); got != "output/test" {
		t.Errorf("stripExt got %q, want %q", got, "output/test")
	}
}
