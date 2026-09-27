package main

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// prefetchResult は先読みの結果（ログ用）。
type prefetchResult struct {
	Files    int
	Bytes    int64
	Elapsed  time.Duration
	TimedOut bool
}

// prefetchFiles は listFile に書かれたパス（ファイルまたはディレクトリ、1行1つ）を読み捨てる。
//
// Lambda のコンテナイメージは必要になった部分から少しずつ読み込まれるため、コールドスタートでは
// Chrome 本体と共有ライブラリの読み込みに数十秒かかることがある（実機で40秒）。
// 初期化フェーズ（Runtime API の /next を呼ぶ前）に読んでおくと、その分が最初の呼び出しから外れる。
// /next を呼ぶと実行環境が止められるので、バックグラウンドではなくここで読み終える。
// 初期化フェーズは10秒までなので budget で打ち切る。
func prefetchFiles(listFile string, budget time.Duration, workers int) prefetchResult {
	start := time.Now()
	deadline := start.Add(budget)

	paths := readPrefetchList(listFile)
	files := make(chan string, 256)
	go func() {
		defer close(files)
		for _, p := range paths {
			_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil || time.Now().After(deadline) {
					return filepath.SkipAll
				}
				if d.Type().IsRegular() {
					files <- path
				}
				return nil
			})
		}
	}()

	var nFiles, nBytes int64
	var timedOut atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1<<20)
			for path := range files {
				if time.Now().After(deadline) {
					timedOut.Store(true)
					continue // 残りは捨てて channel を空にする
				}
				n, ok := readAll(path, buf, deadline)
				atomic.AddInt64(&nBytes, n)
				atomic.AddInt64(&nFiles, 1)
				if !ok {
					timedOut.Store(true)
				}
			}
		}()
	}
	wg.Wait()
	return prefetchResult{Files: int(nFiles), Bytes: nBytes, Elapsed: time.Since(start), TimedOut: timedOut.Load()}
}

// readAll はファイルを最後まで読み捨てる。途中で締め切りを過ぎたら ok=false。
func readAll(path string, buf []byte, deadline time.Time) (n int64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, true
	}
	defer f.Close()
	for {
		if time.Now().After(deadline) {
			return n, false
		}
		m, err := f.Read(buf)
		n += int64(m)
		if err == io.EOF {
			return n, true
		}
		if err != nil {
			return n, true
		}
	}
}

// readPrefetchList は先読みするパスの一覧を読む。空行と # で始まる行は無視する。
func readPrefetchList(listFile string) []string {
	f, err := os.Open(listFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	var paths []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}
