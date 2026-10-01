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

// ChromePathEnv はChrome実行ファイルパスを指定する環境変数名。
const ChromePathEnv = "SITE2PDF_CHROME_PATH"

// FindChrome は利用可能なChrome/Edgeの実行ファイルパスを返す。
// 環境変数 SITE2PDF_CHROME_PATH が設定されていればそれを優先する。
// 見つからない場合は --chrome フラグで指定するよう促すエラーを返す。
func FindChrome() (string, error) {
	if p := os.Getenv(ChromePathEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%s のChromeが見つかりません(%s): %w", ChromePathEnv, p, err)
		}
		return p, nil
	}
	for _, path := range knownChromePaths() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Chrome/Edge が見つかりません。--chrome フラグか環境変数 %s で実行ファイルパスを指定してください", ChromePathEnv)
}
