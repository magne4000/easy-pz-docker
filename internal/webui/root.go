package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the built SPA rooted at dist, so "index.html" is a valid name.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Available reports whether a real build is embedded — false on a Go-only
// checkout, where dist holds only .gitkeep.
func Available() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
