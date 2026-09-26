//go:build linux

package main

import "syscall"

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

// childrenMaxRSSMB は回収済み子孫プロセス（Chrome本体・レンダラー等）のうち最大の常駐メモリをMB単位で返す。
// Linux の ru_maxrss は KB 単位。実行環境が再利用された場合は過去の呼び出しを含む最大値になる。
func childrenMaxRSSMB() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &ru); err != nil {
		return 0
	}
	return float64(ru.Maxrss) / 1024
}
