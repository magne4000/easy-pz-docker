package devfake

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/pz/rcon"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/sys"
)

// ---------- Supervisor ----------

type Supervisor struct {
	o  pz.ProcessOptions
	sc Scenario

	mu      sync.Mutex
	status  pz.Status
	gen     int
	crashes []time.Time
}

func NewSupervisor(o pz.ProcessOptions, sc Scenario) *Supervisor {
	return &Supervisor{o: o, sc: sc, status: pz.Status{State: pz.StateStopped}}
}

var _ pz.Supervisor = (*Supervisor)(nil)

func (s *Supervisor) Status() pz.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	if st.LastExit != nil {
		e := *st.LastExit
		st.LastExit = &e
	}
	return st
}

func (s *Supervisor) set(gen int, st pz.State, mut func(*pz.Status)) bool {
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		return false
	}
	s.status.State = st
	if mut != nil {
		mut(&s.status)
	}
	cp := s.status
	s.mu.Unlock()
	if s.o.Hooks.OnState != nil {
		s.o.Hooks.OnState(cp)
	}
	return true
}

func (s *Supervisor) line(format string, args ...any) {
	if s.o.Hooks.OnLine != nil {
		s.o.Hooks.OnLine(fmt.Sprintf(format, args...))
	}
}

func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	switch s.status.State {
	case pz.StateStarting, pz.StateRunning, pz.StateStopping:
		s.mu.Unlock()
		return pz.ErrAlreadyRunning
	}
	s.gen++
	gen := s.gen
	s.mu.Unlock()
	s.set(gen, pz.StateStarting, func(st *pz.Status) { st.PID = 0 })
	args := []string{"-cachedir=" + s.o.DataDir, "-servername", s.o.ServerName}
	if s.o.PreStart != nil {
		extra, err := s.o.PreStart(ctx)
		if err != nil {
			s.set(gen, pz.StateStopped, func(st *pz.Status) {
				st.LastExit = &pz.ExitInfo{At: time.Now().UTC(), Code: -1, Classification: "start-failed", Message: err.Error()}
			})
			return err
		}
		args = append(args, extra...)
	}
	pid := 4000 + rand.IntN(5000)
	s.set(gen, pz.StateStarting, func(st *pz.Status) { st.PID = pid; st.StartedAt = time.Now().UTC() })
	s.line("[fake] ./start-server.sh %s", strings.Join(args, " "))
	go s.boot(gen)
	return nil
}

func (s *Supervisor) boot(gen int) {
	steps := []string{
		"versionNumber=42.12.0 demo=false",
		"LOG  : General     , > Loading worlddictionary...",
		"LOG  : Network     , > Initialising RakNet...",
		"LOG  : Mod         , > loading mods from Mods=",
		"LOG  : General     , > Loading map chunks...",
	}
	for _, l := range steps {
		time.Sleep(600 * time.Millisecond)
		if s.Status().State != pz.StateStarting {
			return
		}
		s.line("%s", l)
	}
	s.line("*** SERVER STARTED ****")
	if !s.set(gen, pz.StateRunning, nil) {
		return
	}
	go s.live(gen)
}

func (s *Supervisor) current(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen == gen && s.status.State == pz.StateRunning
}

// live writes to the save directory now and then, so the backup change gate
// sees a changing world, and emits log noise.
func (s *Supervisor) live(gen int) {
	start := time.Now()
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	n := 0
	for range tick.C {
		if !s.current(gen) {
			return
		}
		if s.sc.CrashAfter > 0 && time.Since(start) >= s.sc.CrashAfter {
			s.line("# A fatal error has been detected by the Java Runtime Environment: SIGSEGV")
			s.crash(gen)
			return
		}
		n++
		s.line("LOG  : General     , > [fake] tick %d, zombies=%d", n, 1200+rand.IntN(300))
		if n%3 == 0 {
			dir := filepath.Join(s.o.DataDir, "Saves", "Multiplayer", s.o.ServerName)
			os.MkdirAll(dir, 0o755)
			name := fmt.Sprintf("map_%d_%d.bin", rand.IntN(8), rand.IntN(8))
			os.WriteFile(filepath.Join(dir, name), []byte(time.Now().String()), 0o644)
		}
	}
}

func (s *Supervisor) crash(gen int) {
	now := time.Now()
	s.mu.Lock()
	var kept []time.Time
	for _, t := range s.crashes {
		if now.Sub(t) < 10*time.Minute {
			kept = append(kept, t)
		}
	}
	s.crashes = append(kept, now)
	loop := len(s.crashes) >= 3
	s.mu.Unlock()
	s.set(gen, pz.StateCrashed, func(st *pz.Status) {
		st.PID = 0
		st.CrashLoop = loop
		st.LastExit = &pz.ExitInfo{At: now.UTC(), Code: 134, Classification: "crash", Message: "exited unexpectedly with code 134"}
	})
	if s.o.AutoRestart && !loop {
		go func() {
			time.Sleep(5 * time.Second)
			if s.Status().State == pz.StateCrashed {
				s.Start(context.Background())
			}
		}()
	}
}

func (s *Supervisor) ResetCrashLoop() {
	s.mu.Lock()
	s.crashes = nil
	s.status.CrashLoop = false
	s.mu.Unlock()
}

func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	st, gen := s.status.State, s.gen
	s.mu.Unlock()
	if st == pz.StateStopped || st == pz.StateCrashed {
		return nil
	}
	s.set(gen, pz.StateStopping, nil)
	s.line("LOG  : General     , > saving world...")
	time.Sleep(1500 * time.Millisecond)
	s.line("LOG  : General     , > server shutting down")
	s.set(gen, pz.StateStopped, func(st *pz.Status) {
		st.PID = 0
		st.LastExit = &pz.ExitInfo{At: time.Now().UTC(), Code: 0, Classification: "clean", Message: "stopped on request"}
	})
	return nil
}

func (s *Supervisor) SendConsole(cmd string) error {
	if s.Status().State == pz.StateStopped {
		return pz.ErrNotRunning
	}
	s.line("> %s", cmd)
	return nil
}

// ---------- RCON ----------

type RCON struct {
	sup     *Supervisor
	mu      sync.Mutex
	players []string
	log     func(string)
}

func NewRCON(sup *Supervisor, sc Scenario, onLine func(string)) *RCON {
	r := &RCON{sup: sup, players: append([]string{}, sc.Players...), log: onLine}
	if sc.PlayersLeaveAfter > 0 {
		time.AfterFunc(sc.PlayersLeaveAfter, func() {
			r.mu.Lock()
			r.players = []string{}
			r.mu.Unlock()
			if onLine != nil {
				onLine("LOG  : Network     , > [fake] all players disconnected")
			}
		})
	}
	return r
}

var errRefused = errors.New("rcon: dial 127.0.0.1:27015: connect: connection refused")

func (r *RCON) Exec(ctx context.Context, cmd string) (string, error) {
	if r.sup.Status().State != pz.StateRunning {
		return "", errRefused
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", nil
	}
	var resp string
	switch strings.ToLower(fields[0]) {
	case "players":
		r.mu.Lock()
		var b strings.Builder
		fmt.Fprintf(&b, "Players connected (%d):\n", len(r.players))
		for _, p := range r.players {
			b.WriteString("-" + p + "\n")
		}
		r.mu.Unlock()
		resp = b.String()
	case "save":
		resp = "World saved"
	case "servermsg":
		resp = "Message sent."
		if r.log != nil {
			r.log("LOG  : General     , > [broadcast] " + strings.Trim(strings.TrimPrefix(cmd, fields[0]+" "), `"`))
		}
	case "quit":
		resp = "Quit"
	case "reloadoptions":
		resp = "Options reloaded"
	case "help":
		resp = "List of server commands :\n* players\n* save\n* servermsg\n* kickuser\n* quit\n* reloadoptions"
	case "kickuser":
		if len(fields) < 2 {
			resp = "Wrong arguments"
		} else {
			resp = "User " + fields[1] + " doesn't exist."
		}
	default:
		resp = "Unknown command " + fields[0]
	}
	if rej := rcon.Classify(resp); rej != nil {
		return resp, rej
	}
	return resp, nil
}

func (r *RCON) Players(ctx context.Context) ([]string, error) {
	resp, err := r.Exec(ctx, "players")
	if err != nil {
		return nil, err
	}
	return rcon.ParsePlayers(resp), nil
}

// ---------- SteamCMD + Workshop ----------

type CMD struct {
	installDir string
	sc         Scenario
	onLine     func(string)
	mu         sync.Mutex
	latest     string
}

func NewCMD(installDir string, sc Scenario, onLine func(string)) *CMD {
	return &CMD{installDir: installDir, sc: sc, onLine: onLine, latest: sc.LatestBuild}
}

var _ steam.CMD = (*CMD)(nil)

func (c *CMD) say(s string) {
	if c.onLine != nil {
		c.onLine("[steamcmd] " + s)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (c *CMD) AppUpdate(ctx context.Context, branch string, validate bool, onProgress func(steam.Progress)) error {
	if onProgress == nil {
		onProgress = func(steam.Progress) {}
	}
	c.say("Connecting anonymously to Steam Public...OK")
	onProgress(steam.Progress{Phase: "login", Message: "Connecting to Steam"})
	for pct := 0.0; pct <= 100; pct += 12.5 {
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return err
		}
		msg := fmt.Sprintf("Update state (0x61) downloading, progress: %.2f", pct)
		c.say(msg)
		onProgress(steam.Progress{Phase: "downloading", Percent: pct, Message: msg})
	}
	c.mu.Lock()
	build := c.latest
	c.mu.Unlock()
	if err := WriteAppManifest(c.installDir, build, branch); err != nil {
		return err
	}
	c.say("Success! App '380870' fully installed.")
	onProgress(steam.Progress{Phase: "done", Percent: 100, Message: "Game files up to date"})
	return nil
}

func (c *CMD) LatestBuildID(ctx context.Context, branch string) (string, error) {
	if err := sleepCtx(ctx, 300*time.Millisecond); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latest, nil
}

func (c *CMD) InstalledBuild(ctx context.Context) (steam.AppManifest, error) {
	return steam.ReadAppManifest(c.installDir)
}

func (c *CMD) WorkshopDownload(ctx context.Context, ids []string, onProgress func(steam.Progress)) error {
	if onProgress == nil {
		onProgress = func(steam.Progress) {}
	}
	c.say("Connecting anonymously to Steam Public...OK")
	for i, id := range ids {
		if err := sleepCtx(ctx, 700*time.Millisecond); err != nil {
			return err
		}
		it := item(id)
		updated := remoteUpdated(c.sc, id)
		if err := writeItem(c.installDir, id, it); err != nil {
			return err
		}
		if err := c.setWorkshopState(id, &steam.WorkshopItemState{ID: id, Size: it.size, TimeUpdated: updated}); err != nil {
			return err
		}
		msg := fmt.Sprintf("Success. Downloaded item %s to \"%s\" (%d bytes)", id, steam.WorkshopContentDir(c.installDir, id), it.size)
		c.say(msg)
		onProgress(steam.Progress{Phase: "workshop", Percent: 100 * float64(i+1) / float64(len(ids)), Message: msg})
	}
	return nil
}

func (c *CMD) WorkshopInstalled(ctx context.Context) (map[string]steam.WorkshopItemState, error) {
	return steam.ReadWorkshopManifest(c.installDir)
}

func (c *CMD) WorkshopRemove(ctx context.Context, id string) error {
	if err := os.RemoveAll(steam.WorkshopContentDir(c.installDir, id)); err != nil {
		return err
	}
	return c.setWorkshopState(id, nil)
}

func (c *CMD) setWorkshopState(id string, st *steam.WorkshopItemState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	all, err := steam.ReadWorkshopManifest(c.installDir)
	if err != nil {
		return err
	}
	if st == nil {
		delete(all, id)
	} else {
		all[id] = *st
	}
	return writeWorkshopManifest(c.installDir, all)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func writeItem(installDir, id string, it catalogItem) error {
	root := steam.WorkshopContentDir(installDir, id)
	os.RemoveAll(root)
	for _, m := range it.mods {
		dir := filepath.Join(root, "mods", m.id)
		info := fmt.Sprintf("name=%s\nid=%s\ndescription=Fake mod generated by pzman dev drivers\n", m.name, m.id)
		if len(m.requires) > 0 {
			reqs := make([]string, len(m.requires))
			for i, r := range m.requires {
				reqs[i] = `\` + r
			}
			info += "require=" + strings.Join(reqs, ",") + "\n"
		}
		files := map[string]string{"mod.info": info}
		for _, f := range m.files {
			files[f] = "-- " + m.id + "\n"
		}
		for _, mp := range m.maps {
			files[filepath.Join("media", "maps", mp, "map.info")] = "title=" + mp + "\n"
		}
		for name, content := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeWorkshopManifest(installDir string, items map[string]steam.WorkshopItemState) error {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("\"AppWorkshop\"\n{\n\t\"appid\"\t\t\"108600\"\n\t\"WorkshopItemsInstalled\"\n\t{\n")
	for _, id := range ids {
		it := items[id]
		fmt.Fprintf(&b, "\t\t\"%s\"\n\t\t{\n\t\t\t\"size\"\t\t\"%d\"\n\t\t\t\"timeupdated\"\t\t\"%d\"\n\t\t\t\"manifest\"\t\t\"%d\"\n\t\t}\n",
			id, it.Size, it.TimeUpdated.Unix(), it.TimeUpdated.Unix()*7)
	}
	b.WriteString("\t}\n}\n")
	p := filepath.Join(installDir, "steamapps", "workshop", "appworkshop_108600.acf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

func WriteAppManifest(installDir, build, branch string) error {
	p := filepath.Join(installDir, "steamapps", "appmanifest_380870.acf")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	beta := ""
	if branch != "" {
		beta = fmt.Sprintf("\t\"UserConfig\"\n\t{\n\t\t\"BetaKey\"\t\t\"%s\"\n\t}\n", branch)
	}
	content := fmt.Sprintf("\"AppState\"\n{\n\t\"appid\"\t\t\"380870\"\n\t\"name\"\t\t\"Project Zomboid Dedicated Server\"\n\t\"StateFlags\"\t\t\"4\"\n\t\"buildid\"\t\t\"%s\"\n\t\"LastUpdated\"\t\t\"%d\"\n\t\"SizeOnDisk\"\t\t\"2147483648\"\n%s}\n",
		build, time.Now().Unix(), beta)
	return os.WriteFile(p, []byte(content), 0o644)
}

// ---------- Steam Web API ----------

var started = time.Now()

// remoteUpdated is the item's Workshop version; the scenario's OutdatedMods get
// a new release 20s after startup, so an update is detectable after boot.
func remoteUpdated(sc Scenario, id string) time.Time {
	t := item(id).updated
	if contains(sc.OutdatedMods, id) && time.Since(started) > 20*time.Second {
		t = t.Add(24 * time.Hour)
	}
	return t
}

type WebAPI struct{ Scenario Scenario }

var _ steam.WebAPI = WebAPI{}

func (w WebAPI) PublishedFileDetails(ctx context.Context, ids []string) ([]steam.FileDetails, error) {
	out := make([]steam.FileDetails, 0, len(ids))
	for _, id := range ids {
		it := item(id)
		out = append(out, steam.FileDetails{ID: id, Title: it.title, FileSize: it.size, TimeUpdated: remoteUpdated(w.Scenario, id), Result: 1, Tags: []string{"Build 42"}})
	}
	return out, nil
}

func (WebAPI) CollectionDetails(ctx context.Context, id string) ([]string, error) {
	if ids, ok := collections[id]; ok {
		return ids, nil
	}
	return nil, fmt.Errorf("%w: %s", steam.ErrCollectionNotFound, id)
}

// ---------- disk ----------

// DiskUsage reports the scenario's fill level for the data volume.
func DiskUsage(sc Scenario) func(string) (sys.DiskUsage, error) {
	return func(path string) (sys.DiskUsage, error) {
		u, err := sys.Usage(path)
		if err != nil {
			return u, err
		}
		if sc.DiskUsedPct > 0 {
			const total = 2_000_000_000_000
			used := uint64(float64(total) * sc.DiskUsedPct / 100)
			u = sys.DiskUsage{Path: path, Total: total, Used: used, Free: total - used, UsedPercent: sc.DiskUsedPct}
		}
		return u, nil
	}
}
