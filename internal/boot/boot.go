// Package boot wires every subsystem together and owns the process lifecycle:
// boot order, and SIGTERM → save → quit → await JVM → exit.
package boot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/console"
	"github.com/magne4000/easy-pz-docker/internal/devfake"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/httpapi"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/pz/rcon"
	"github.com/magne4000/easy-pz-docker/internal/sched"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/sys"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

type drivers struct {
	sup  pz.Supervisor
	rcon sched.RCON
	cmd  steam.CMD
	web  steam.WebAPI
	sc   *devfake.Scenario
}

// Run boots pzman and blocks until ctx is cancelled (SIGTERM) and shutdown completes.
func Run(ctx context.Context, cfg app.Config, log *slog.Logger, version string) error {
	fake := cfg.Drivers == app.DriversFake
	var scenario devfake.Scenario
	if fake {
		var err error
		if scenario, err = devfake.Lookup(cfg.Scenario); err != nil {
			return err
		}
		log.Info("fake drivers", "scenario", scenario.Name, "about", scenario.Description)
	}
	for _, d := range []string{cfg.InstallDir, cfg.DataDir, cfg.BackupDir, cfg.CacheDir, filepath.Dir(cfg.DBPath)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	if !fake {
		if err := os.MkdirAll(cfg.SteamHome, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", cfg.SteamHome, err)
		}
	}
	_, statErr := os.Stat(cfg.DBPath)
	freshDB := errors.Is(statErr, fs.ErrNotExist)
	if fake {
		if err := devfake.Seed(cfg.InstallDir, cfg.DataDir, cfg.ServerName, scenario); err != nil {
			return fmt.Errorf("seed dev tree: %w", err)
		}
	}

	db, err := store.Open(ctx, log, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	bus := events.NewBus(log, 256)
	ring := console.NewRing(cfg.ConsoleRing)
	tk := tasks.NewRegistry(bus)
	st, err := settings.Open(ctx, cfg, db, bus)
	if err != nil {
		return err
	}
	consoleLine := func(s string) { bus.Publish(ring.Append(s)) }

	var coord *sched.Coordinator
	hooks := pz.Hooks{
		OnState: func(s pz.Status) {
			log.Info("server state", "state", s.State)
			bus.Publish(events.ServerStatus{State: string(s.State)})
		},
		OnLine: consoleLine,
	}
	popts := pz.ProcessOptions{
		InstallDir: cfg.InstallDir, DataDir: cfg.DataDir, ServerName: cfg.ServerName, UID: cfg.PUID, GID: cfg.PGID,
		StopTimeout: cfg.StopTimeout, AutoRestart: true, Hooks: hooks, Log: log.With("component", "supervisor"),
		PreStart: func(ctx context.Context) ([]string, error) { return coord.PreStart(ctx) },
	}
	var dr drivers
	if fake {
		fs := devfake.NewSupervisor(popts, scenario)
		dr = drivers{sup: fs, rcon: devfake.NewRCON(fs, scenario, consoleLine), cmd: devfake.NewCMD(cfg.InstallDir, scenario, consoleLine),
			web: devfake.WebAPI{Scenario: scenario}, sc: &scenario}
	} else {
		mgr := rcon.NewManager(log.With("component", "rcon"), func() (string, string) { return coord.RCONTarget() })
		defer mgr.Close()
		popts.Quit = func(ctx context.Context) error {
			_, err := mgr.Exec(ctx, "quit")
			mgr.Close()
			return err
		}
		proc := pz.NewProcess(popts)
		dr = drivers{sup: proc, rcon: mgr, web: steam.NewWebAPI(nil, "")}
		dr.cmd = steam.NewSteamCMD(steam.Options{
			Bin: cfg.SteamCMD, InstallDir: cfg.InstallDir, Home: cfg.SteamHome, UID: cfg.PUID, GID: cfg.PGID,
			Log:    log.With("component", "steamcmd"),
			OnLine: func(l string) { consoleLine("[steamcmd] " + l) },
			Guard: func() error { // app_update only (see steam.Options.Guard)
				if s := proc.Status().State; s != pz.StateStopped && s != pz.StateCrashed {
					return errors.New("refusing to run SteamCMD while the game server is " + string(s))
				}
				return nil
			},
		})
	}

	modSvc := mods.NewService(mods.Options{
		InstallDir: cfg.InstallDir, DataDir: cfg.DataDir, ServerName: cfg.ServerName, NonSteam: !cfg.UseSteam,
		DB: db, CMD: dr.cmd, WebAPI: dr.web, Bus: bus, Tasks: tk, Log: log.With("component", "mods"),
		Loaded:     func() []string { return coord.Loaded() },
		Collection: func() string { return st.Get().WorkshopCollection },
	})
	bopts := backup.ServiceOptions{
		DB: db, Bus: bus, Tasks: tk, Log: log.With("component", "backup"),
		Set: backup.DefaultSet(cfg.DataDir, cfg.ServerName), Dir: cfg.BackupDir, ServerName: cfg.ServerName,
		BuildID: func() string { return coord.UpdateStatus().InstalledBuild },
		Mods:    func(ctx context.Context) []backup.ModRef { return modRefs(ctx, modSvc) },
		Policy: func() backup.Policy {
			s := st.Get()
			return backup.Policy{Keep: s.BackupKeep, KeepDaily: s.BackupKeepDaily, KeepWeekly: s.BackupKeepWeekly,
				MaxTotalBytes: int64(s.BackupMaxTotalGB * (1 << 30))}
		},
	}
	if fake && scenario.SlowBackup {
		bopts.OnProgress = func(backup.Progress) { time.Sleep(150 * time.Millisecond) }
	}
	bak := backup.NewService(bopts)
	saveWait := 10 * time.Second
	if fake {
		saveWait = time.Second
	}
	coord = sched.NewCoordinator(sched.Options{Cfg: cfg, Sup: dr.sup, RCON: dr.rcon, CMD: dr.cmd, Mods: modSvc, Backups: bak,
		Settings: st, Bus: bus, Tasks: tk, Log: log.With("component", "coordinator"), SaveWait: saveWait})
	scheduler, err := sched.NewScheduler(coord, db, st, bus, cfg.Location(), nil, log.With("component", "scheduler"))
	if err != nil {
		return err
	}
	disk := sys.NewMonitor(sys.MonitorConfig{
		Paths:   map[string]string{"data": cfg.DataDir, "install": cfg.InstallDir, "backups": cfg.BackupDir},
		WarnPct: cfg.DiskWarnPct, CritPct: cfg.DiskCritPct, Interval: time.Minute, Log: log.With("component", "disk"),
		OnChange: func(ss []sys.Volume) { bus.Publish(events.DiskStatus{Level: string(sys.Worst(ss))}) },
	})

	booted := make(chan struct{})
	srv, err := httpapi.New(cfg, log.With("component", "http"), httpapi.Deps{
		Cfg: cfg, Version: version, Bus: bus, Ring: ring, Store: db, Coord: coord, Sched: scheduler, Mods: modSvc,
		Packer: mods.NewPacker(cfg.CacheDir, log.With("component", "modpack")), Backups: bak, Settings: st, Tasks: tk, Disk: disk,
		Ready: func(ctx context.Context) error {
			select {
			case <-booted:
			default:
				return errors.New("booting")
			}
			return db.Ping(ctx)
		},
	})
	if err != nil {
		return err
	}

	runCtx, stopAll := context.WithCancel(context.Background())
	defer stopAll()
	g, gctx := errgroup.WithContext(runCtx)
	g.Go(func() error { return srv.Run(gctx) })
	g.Go(func() error { disk.Run(gctx); return nil })
	if err := scheduler.Start(gctx); err != nil {
		return err
	}

	go func() {
		defer close(booted)
		env := bootEnv{cfg: cfg, log: log, coord: coord, mods: modSvc, dr: dr, tasks: tk, fake: fake, freshDB: freshDB}
		if err := bootSequence(ctx, env); err != nil {
			log.Error("boot sequence failed; the panel stays up so you can fix it", "err", err)
			consoleLine("[pzman] boot failed: " + err.Error())
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case <-gctx.Done():
		log.Error("a core component stopped unexpectedly")
	}
	shutdown(log, cfg, dr.sup, scheduler, srv)
	stopAll()
	err = g.Wait()
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	log.Info("bye")
	return err
}

func modRefs(ctx context.Context, m *mods.Service) []backup.ModRef {
	ms, installed, err := m.Enabled(ctx)
	if err != nil {
		return []backup.ModRef{}
	}
	byItem := map[string]*backup.ModRef{}
	var out []backup.ModRef
	var order []string
	for _, mi := range ms {
		r, ok := byItem[mi.WorkshopID]
		if !ok {
			r = &backup.ModRef{WorkshopID: mi.WorkshopID, TimeUpdated: installed[mi.WorkshopID].TimeUpdated}
			byItem[mi.WorkshopID] = r
			order = append(order, mi.WorkshopID)
		}
		r.ModIDs = append(r.ModIDs, mi.ID)
	}
	for _, id := range order {
		out = append(out, *byItem[id])
	}
	return out
}

// bootEnv is what the boot sequence needs from Run's wiring.
type bootEnv struct {
	cfg     app.Config
	log     *slog.Logger
	coord   *sched.Coordinator
	mods    *mods.Service
	dr      drivers
	tasks   *tasks.Registry
	fake    bool // PANEL_DRIVERS=fake
	freshDB bool // the database was created by this start
}

// bootSequence: ownership fix → install/update game → fetch missing mods →
// start (PreStart applies launch JSON, ini seeds, mod links and the self-check).
func bootSequence(ctx context.Context, b bootEnv) error {
	if !b.fake && !b.cfg.IsDev() {
		// SteamCMD updates itself in place, so its own directory must belong to PUID too.
		for _, root := range []string{b.cfg.InstallDir, b.cfg.DataDir, b.cfg.BackupDir, b.cfg.SteamHome, b.cfg.CacheDir, filepath.Dir(b.cfg.SteamCMD)} {
			res, err := sys.FixOwnership(ctx, root, b.cfg.PUID, b.cfg.PGID)
			if err != nil {
				return err
			}
			if res.Fixed > 0 || len(res.Errors) > 0 {
				b.log.Info("ownership fixed", "root", root, "checked", res.Checked, "fixed", res.Fixed, "errors", len(res.Errors))
			}
			for _, e := range res.Errors {
				b.log.Warn("chown", "err", e)
			}
		}
	}
	if b.fake && b.freshDB && b.dr.sc != nil {
		if err := devfake.Setup(ctx, b.mods, *b.dr.sc, true); err != nil {
			return err
		}
	}
	if b.cfg.IsDev() && !b.fake {
		b.log.Info("dev mode with real drivers: not installing or starting the game server")
		b.coord.RefreshInstalled(ctx)
		return nil
	}
	_, err := b.dr.cmd.InstalledBuild(ctx)
	installed := err == nil
	if !installed || (b.cfg.UpdateOnStart && !b.fake) {
		h := b.tasks.Start("game-update", "Installing / updating game files")
		err := b.dr.cmd.AppUpdate(ctx, b.cfg.ServerBranch, !installed, func(p steam.Progress) { h.Progress(p.Percent, p.Message) })
		h.Finish(err)
		if err != nil {
			if !installed {
				return fmt.Errorf("game install failed: %w", err)
			}
			b.log.Warn("game update at start failed; starting the installed build", "err", err)
		}
	}
	b.coord.RefreshInstalled(ctx)
	if _, err := b.mods.CheckUpdates(ctx); err != nil {
		b.log.Warn("workshop metadata check failed", "err", err)
	}
	h := b.tasks.Start("workshop", "Fetching missing or outdated mods")
	ids, err := b.mods.DownloadOutdated(ctx, h)
	h.Finish(err)
	if err != nil {
		b.log.Warn("workshop download at boot failed", "err", err)
	} else if len(ids) > 0 {
		b.log.Info("workshop items downloaded", "count", len(ids))
	}
	if err := b.coord.Start(ctx); err != nil {
		return err
	}
	if b.fake && b.dr.sc != nil && b.dr.sc.TriggerBackup {
		go func() {
			time.Sleep(8 * time.Second)
			if _, err := b.coord.RunBackup(context.Background(), "manual", "scenario backup-running", true); err != nil {
				b.log.Warn("scenario backup", "err", err)
			}
		}()
	}
	go func() {
		if _, err := b.coord.CheckUpdates(context.Background()); err != nil {
			b.log.Warn("initial update check", "err", err)
		}
	}()
	return nil
}

// shutdown is the PID 1 path: stop jobs, save + quit the JVM and wait for it,
// then stop serving HTTP.
func shutdown(log *slog.Logger, cfg app.Config, sup pz.Supervisor, scheduler *sched.Scheduler, srv *httpapi.Server) {
	if err := scheduler.Shutdown(); err != nil {
		log.Warn("scheduler shutdown", "err", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StopTimeout+30*time.Second)
	defer cancel()
	if s := sup.Status().State; s != pz.StateStopped && s != pz.StateCrashed {
		log.Info("stopping game server")
		if err := sup.Stop(ctx); err != nil {
			log.Error("game server stop", "err", err)
		}
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hcancel()
	if err := srv.Shutdown(hctx); err != nil {
		log.Warn("http shutdown", "err", err)
	}
}
