package server

import "testing"

func TestLocalizeErrorMessage(t *testing.T) {
	if got := localizeErrorMessage("路径无效或不是 Markdown 文件", 400); got != "Invalid path or not a Markdown file." {
		t.Fatalf("unexpected translation: %q", got)
	}
	if got := localizeErrorMessage("无法读取目录：permission denied", 403); got != "Could not read directory: permission denied" {
		t.Fatalf("unexpected prefix translation: %q", got)
	}
	if got := localizeErrorMessage("some English detail", 500); got != "some English detail" {
		t.Fatalf("English should pass through: %q", got)
	}
	if got := localizeErrorMessage("未知中文错误", 400); got != "Request failed. Check the input and try again." {
		t.Fatalf("unexpected fallback: %q", got)
	}
}
