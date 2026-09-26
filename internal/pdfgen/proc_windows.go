//go:build windows

package pdfgen

import "os/exec"

// setProcessGroup は Windows では何もしない（従来どおりの起動）。
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessTree は Windows では従来どおりメインプロセスのみ終了する。
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
