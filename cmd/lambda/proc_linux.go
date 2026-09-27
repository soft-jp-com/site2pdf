//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// prSetChildSubreaper は prctl(2) の PR_SET_CHILD_SUBREAPER。
const prSetChildSubreaper = 36

// enableSubreaper はこのプロセスを子孫プロセスの引き取り役(subreaper)にする。
// Chrome のレンダラー等は親(Chrome本体)を失うと PID 1 に引き取られるが、
// RIE の PID 1 は回収しないためゾンビとして残り続ける。
// subreaper にしておけば孤児はこのプロセスに引き取られ、reapZombies で回収できる。
// Lambda 実機では prctl が許可されず失敗する（operation not permitted）。その場合は procStats のログで様子を見る。
func enableSubreaper() error {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// reapZombies は終了済みの子プロセスをすべて回収する（待たない）。
func reapZombies() int {
	n := 0
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			return n
		}
		n++
	}
}

// memoryPeakMB は実行環境（cgroup）全体の最大メモリ使用量をMB単位で返す。
// プロセス単位の ru_maxrss と違い、Chrome のレンダラー等を含めた合計の最大値になる。
// cgroup v2 の memory.peak が読めない環境（Lambda 実機では読めなかった）では 0 を返す。
// 実行環境が再利用された場合は過去の呼び出しを含む最大値になる。
func memoryPeakMB() float64 {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.peak")
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return float64(n) / (1024 * 1024)
}

// procStats は見えているプロセスのうち、Chrome の生存数とゾンビ数を数える。
// 実行環境の再利用でプロセスが溜まっていないかをログで確かめるために使う。
func procStats() (chromeAlive, zombies int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// 形式: pid (comm) state ...  comm に空白や括弧が入り得るので最後の ')' で区切る
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 || i+2 >= len(s) {
			continue
		}
		state := s[i+2]
		if state == 'Z' {
			zombies++
		} else if strings.Contains(s[:i], "chrome") {
			chromeAlive++
		}
	}
	return chromeAlive, zombies
}
