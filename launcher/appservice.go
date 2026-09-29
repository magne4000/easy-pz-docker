package main

import (
	"context"
	"os"
	"os/exec"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/magne4000/easy-pz-docker/launcher/internal/selfupdate"
)

type AppService struct {
	version string
	app     *application.App
	window  *application.WebviewWindow
	updater *selfupdate.Updater

	mu      sync.Mutex
	pending *selfupdate.Release
}

func (a *AppService) Version() string { return a.version }

func (a *AppService) ChooseFolder(title string) (string, error) {
	return a.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

func (a *AppService) OpenURL(url string) error {
	return a.app.Browser.OpenURL(url)
}

type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
}

func (a *AppService) CheckUpdate(ctx context.Context) (UpdateInfo, error) {
	rel, err := a.updater.Check(ctx)
	if err != nil || rel == nil {
		return UpdateInfo{}, err
	}
	a.mu.Lock()
	a.pending = rel
	a.mu.Unlock()
	return UpdateInfo{Available: true, Version: rel.Version, URL: rel.URL}, nil
}

func (a *AppService) ApplyUpdate(ctx context.Context) error {
	a.mu.Lock()
	rel := a.pending
	a.mu.Unlock()
	if rel == nil {
		return nil
	}
	path, err := a.updater.Apply(ctx, rel)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		cmd = exec.Command("open", "-n", path) // macOS .app bundle
	} else {
		cmd = exec.Command(path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	a.app.Quit()
	return nil
}
