package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrefetchFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "chrome")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, size := range []int{10, 2 << 20, 0} {
		p := filepath.Join(sub, string(rune('a'+i)))
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	single := filepath.Join(dir, "lib.so")
	if err := os.WriteFile(single, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(dir, "prefetch.list")
	content := "# コメント\n" + sub + "\n\n" + single + "\n" + filepath.Join(dir, "missing") + "\n"
	if err := os.WriteFile(list, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := prefetchFiles(list, 5*time.Second, 4)
	if r.Files != 4 || r.Bytes != 10+(2<<20)+100 || r.TimedOut {
		t.Errorf("files=%d bytes=%d timedOut=%v", r.Files, r.Bytes, r.TimedOut)
	}

	// 締め切りを過ぎていれば読まずに打ち切る
	r = prefetchFiles(list, 0, 4)
	if r.Bytes != 0 {
		t.Errorf("budget 0 で %d bytes 読んでいる", r.Bytes)
	}

	// 一覧ファイルが無くても落ちない
	if r := prefetchFiles(filepath.Join(dir, "none"), time.Second, 2); r.Files != 0 {
		t.Errorf("files=%d", r.Files)
	}
}
