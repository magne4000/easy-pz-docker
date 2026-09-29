package webui

import (
	"io/fs"
	"testing"
)

func TestAvailableMatchesIndex(t *testing.T) {
	_, err := fs.Stat(FS(), "index.html")
	if Available() != (err == nil) {
		t.Fatalf("Available()=%v but index.html stat err=%v", Available(), err)
	}
}
