package main

import (
	"bufio"
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

// prefetchChunkSize は1回に読む範囲の大きさ。大きなファイル（Chrome 本体は263MB）も
// この単位に分けて並列に読む。Lambda のイメージは必要な部分ごとに取ってくるため、
// 1ファイルを先頭から順に読むより、範囲を分けて同時に読むほうが速い。
const prefetchChunkSize = 16 << 20

// prefetchChunk は先読みする範囲（ファイルの一部）。
type prefetchChunk struct {
	path   string
	offset int64
	length int64
}

// prefetchFiles は listFile に書かれたパス（ファイルまたはディレクトリ、1行1つ）を読み捨てる。
//
// Lambda のコンテナイメージは必要になった部分から少しずつ読み込まれるため、コールドスタートでは
// Chrome 本体と共有ライブラリの読み込みに数十秒かかることがある（実機で40秒）。
// 初期化フェーズ（Runtime API の /next を呼ぶ前）に読んでおくと、その分が最初の呼び出しから外れる。
// /next を呼ぶと実行環境が止められるので、バックグラウンドではなくここで読み終える。
// 初期化フェーズは10秒までなので budget で打ち切る。一覧の上にあるものから順に読む。
func prefetchFiles(listFile string, budget time.Duration, workers int) prefetchResult {
	return prefetchFilesWith(listFile, budget, workers, readRange)
}

// prefetchFilesWith は範囲を読む関数を指定して先読みする（テストで読み込みが止まる状況を作るため）。
func prefetchFilesWith(listFile string, budget time.Duration, workers int,
	read func(c prefetchChunk, buf []byte, deadline time.Time) (int64, bool)) prefetchResult {
	start := time.Now()
	deadline := start.Add(budget)

	paths := readPrefetchList(listFile)
	chunks := make(chan prefetchChunk, 256)
	var nFiles int64
	go func() {
		defer close(chunks)
		for _, p := range paths {
			_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil || time.Now().After(deadline) {
					return filepath.SkipAll
				}
				if !d.Type().IsRegular() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				atomic.AddInt64(&nFiles, 1)
				for off := int64(0); off < info.Size(); off += prefetchChunkSize {
					chunks <- prefetchChunk{path: path, offset: off, length: min(prefetchChunkSize, info.Size()-off)}
				}
				return nil
			})
		}
	}()

	var nBytes int64
	var timedOut atomic.Bool
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1<<20)
			for c := range chunks {
				if time.Now().After(deadline) {
					timedOut.Store(true)
					continue // 残りは捨てて channel を空にする
				}
				n, ok := read(c, buf, deadline)
				atomic.AddInt64(&nBytes, n)
				if !ok {
					timedOut.Store(true)
				}
			}
		}()
	}
	// 読み込み中の ReadAt は、イメージの取得待ちで締め切りを過ぎても戻らないことがある。
	// 全部の終了を待つと初期化フェーズが延びるため、締め切りが来たら待たずに戻る。
	// 残った goroutine は /next で実行環境ごと止められ、再開後に締め切り判定に当たって抜けるだけで害はない。
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		timedOut.Store(true)
	}
	return prefetchResult{Files: int(atomic.LoadInt64(&nFiles)), Bytes: atomic.LoadInt64(&nBytes), Elapsed: time.Since(start), TimedOut: timedOut.Load()}
}

// readRange はファイルの指定範囲を読み捨てる。途中で締め切りを過ぎたら ok=false。
func readRange(c prefetchChunk, buf []byte, deadline time.Time) (n int64, ok bool) {
	f, err := os.Open(c.path)
	if err != nil {
		return 0, true
	}
	defer f.Close()
	for n < c.length {
		if time.Now().After(deadline) {
			return n, false
		}
		want := min(int64(len(buf)), c.length-n)
		m, err := f.ReadAt(buf[:want], c.offset+n)
		n += int64(m)
		if err != nil { // io.EOF を含む（読んでいる間にファイルが縮んだ場合など）
			return n, true
		}
	}
	return n, true
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
