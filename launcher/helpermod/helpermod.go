package helpermod

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const ID = "EasyPZAutoConnect"

//go:embed all:EasyPZAutoConnect
var files embed.FS

func Install(modsDir string) error {
	dst := filepath.Join(modsDir, ID)
	if upToDate(dst) {
		return nil
	}
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(modsDir, "."+ID+".tmp*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copyTo(tmp); err != nil {
		return fmt.Errorf("write helper mod: %w", err)
	}
	old := dst + ".old"
	os.RemoveAll(old)
	if err := os.Rename(dst, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Rename(old, dst)
		return err
	}
	return os.RemoveAll(old)
}

func copyTo(root string) error {
	return fs.WalkDir(files, ID, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(ID, filepath.FromSlash(p))
		target := filepath.Join(root, rel)
		if e.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func upToDate(dst string) bool {
	want := map[string][]byte{}
	_ = fs.WalkDir(files, ID, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			rel, _ := filepath.Rel(ID, filepath.FromSlash(p))
			want[rel], _ = files.ReadFile(p)
		}
		return nil
	})
	n := 0
	ok := filepath.WalkDir(dst, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		exp, known := want[rel]
		got, err := os.ReadFile(p)
		if err != nil || !known || !bytes.Equal(got, exp) {
			return fs.ErrInvalid
		}
		n++
		return nil
	}) == nil
	return ok && n == len(want)
}
