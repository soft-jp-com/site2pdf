package pdfgen

import (
	"fmt"
	"os"
	"path/filepath"
)

// knownChromePaths はWindows上のChrome/Edgeの既知インストールパス一覧を返す。
// 副作用: 環境変数 LOCALAPPDATA を参照する。
func knownChromePaths() []string {
	localAppData := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("PROGRAMFILES")
	programFilesX86 := os.Getenv("PROGRAMFILES(X86)")

	paths := []string{
		filepath.Join(programFiles, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(programFilesX86, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(localAppData, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(programFilesX86, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(programFiles, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(localAppData, `Microsoft\Edge\Application\msedge.exe`),
	}
	return paths
}

// FindChrome は利用可能なChrome/Edgeの実行ファイルパスを返す。
// 見つからない場合は --chrome フラグで指定するよう促すエラーを返す。
func FindChrome() (string, error) {
	for _, path := range knownChromePaths() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Chrome/Edge が見つかりません。--chrome フラグで実行ファイルパスを指定してください")
}
