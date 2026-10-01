package pdfgen

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPageContentJSON(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	content := PageContent{
		URL:         "https://example.com/ir/news/1",
		FinalURL:    "https://example.com/ir/news/1",
		Title:       "Q3 2026 Financial Results",
		SiteName:    "Example Corp IR",
		Description: "Example Corp reports strong Q3 results.",
		PublishedAt: "2026-08-30T10:00:00Z",
		Article:     "Main article content without header and footer.",
		Text:        "All page innerText.",
		FetchedAt:   now,
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PageContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Title != content.Title {
		t.Errorf("Title = %q, want %q", decoded.Title, content.Title)
	}
	if decoded.Article != content.Article {
		t.Errorf("Article = %q, want %q", decoded.Article, content.Article)
	}
	if decoded.PublishedAt != content.PublishedAt {
		t.Errorf("PublishedAt = %q, want %q", decoded.PublishedAt, content.PublishedAt)
	}
}

func TestReplaceOrAppendExt(t *testing.T) {
	tests := []struct {
		path     string
		ext      string
		expected string
	}{
		{"output/file.pdf", ".json", "output/file.json"},
		{"output/file", ".json", "output/file.json"},
		{"output/file.txt", ".pdf", "output/file.pdf"},
		{"output/file.tar.gz", ".json", "output/file.tar.json"},
	}

	for _, tt := range tests {
		got := replaceOrAppendExt(tt.path, tt.ext)
		if got != tt.expected {
			t.Errorf("replaceOrAppendExt(%q, %q) = %q, want %q", tt.path, tt.ext, got, tt.expected)
		}
	}
}
