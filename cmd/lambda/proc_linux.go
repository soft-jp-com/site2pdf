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
// Lambda/RIE の PID 1 は回収しないためゾンビとして残り続ける。
// subreaper にしておけば孤児はこのプロセスに引き取られ、reapZombies で回収できる。
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
// cgroup v2 の memory.peak が読めない環境（Lambda 実機では未確認）では 0 を返す。
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
