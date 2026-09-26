//go:build !linux

package main

// enableSubreaper は Linux 以外では何もしない。
func enableSubreaper() error { return nil }

// reapZombies は Linux 以外では何もしない。
func reapZombies() int { return 0 }

// childrenMaxRSSMB は Linux 以外では計測しない。
func childrenMaxRSSMB() float64 { return 0 }
