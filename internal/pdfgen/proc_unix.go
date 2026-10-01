//go:build !windows

package pdfgen

import (
	"os/exec"
	"syscall"
)

// setProcessGroup はChromeを新しいプロセスグループで起動する設定を行う。
// Lambda のように実行環境が再利用される場合、レンダラー等の子プロセスが残ると
// 次の呼び出しでメモリを圧迫するため、グループ単位で終了できるようにする。
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree はChromeとその子プロセスをプロセスグループごと終了する。
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// 負のPIDはプロセスグループ全体を指す
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
