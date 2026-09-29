package main

import (
	"embed"
	"log"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/magne4000/easy-pz-docker/launcher/internal/config"
	"github.com/magne4000/easy-pz-docker/launcher/internal/core"
	"github.com/magne4000/easy-pz-docker/launcher/internal/selfupdate"
	"github.com/magne4000/easy-pz-docker/launcher/internal/zomboid"
)

var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[core.SyncProgress](core.EventSyncProgress)
	application.RegisterEvent[core.Phase](core.EventPlayPhase)
}

type emitter struct{ app *application.App }

func (e *emitter) Emit(name string, data any) {
	if e.app != nil {
		e.app.Event.Emit(name, data)
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	selfupdate.CleanupOld()

	cfgPath, err := config.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	store, err := config.Open(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	user, err := zomboid.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	em := &emitter{}
	svc := core.New(version, store, user, em, logger)
	appSvc := &AppService{version: version, updater: selfupdate.New(version)}

	app := application.New(application.Options{
		Name:        "EasyPZ Launcher",
		Description: "Keeps Project Zomboid mods in sync with a pzman server and joins it",
		Logger:      logger,
		Services: []application.Service{
			application.NewService(svc),
			application.NewService(appSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.github.magne4000.easypz-launcher",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w := appSvc.window; w != nil {
					w.Restore()
					w.Focus()
				}
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	em.app = app
	appSvc.app = app

	appSvc.window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "EasyPZ Launcher",
		Width:     960,
		Height:    680,
		MinWidth:  720,
		MinHeight: 520,
		URL:       "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
