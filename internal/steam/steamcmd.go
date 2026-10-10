package steam

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/sys"
)

type Progress struct {
	Phase   string  `json:"phase"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

type CMD interface {
	AppUpdate(ctx context.Context, branch string, validate bool, onProgress func(Progress)) error
	LatestBuildID(ctx context.Context, branch string) (string, error)
	InstalledBuild(ctx context.Context) (AppManifest, error)
	WorkshopDownload(ctx context.Context, items []WorkshopItem, onProgress func(Progress)) error
	WorkshopInstalled(ctx context.Context) (map[string]WorkshopItemState, error)
	WorkshopRemove(ctx context.Context, id string) error
}

// WorkshopItem is one item for WorkshopDownload. Title and Size come from the
// Workshop (empty when unknown): steamcmd prints nothing while it downloads an
// item, so progress is the bytes it has staged against Size.
type WorkshopItem struct {
	ID    string
	Title string
	Size  int64
}

type Options struct {
	Bin        string
	InstallDir string
	Home       string
	UID, GID   int
	Stall      time.Duration
	// Guard is consulted before app_update: game files must never be replaced
	// under a live JVM. Workshop downloads are not guarded here — fetching a new
	// item is safe while the server runs; mods.Service decides what may be fetched when.
	Guard  func() error
	OnLine func(line string)
	Log    *slog.Logger
}

type runFunc func(ctx context.Context, args []string) (io.ReadCloser, func() error, error)

type SteamCMD struct {
	o         Options
	mu        sync.Mutex // one steamcmd at a time: they share a lock file
	run       runFunc
	pollEvery time.Duration // how often a workshop download's staged bytes are measured
}

func NewSteamCMD(o Options) *SteamCMD {
	if o.Stall <= 0 {
		o.Stall = 10 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	s := &SteamCMD{o: o, pollEvery: time.Second}
	s.run = s.execRun
	return s
}

var _ CMD = (*SteamCMD)(nil)

func (s *SteamCMD) execRun(ctx context.Context, args []string) (io.ReadCloser, func() error, error) {
	proc, err := sys.Proc{Name: "steamcmd", Bin: s.o.Bin, Args: args, Dir: s.o.Home, Home: s.o.Home, UID: s.o.UID, GID: s.o.GID}.Start(ctx)
	if err != nil {
		return nil, nil, err
	}
	return proc.Output, func() error { return <-proc.Done }, nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

var ErrStalled = errors.New("steamcmd: no output or download progress within the stall timeout, killed")

// invoke runs steamcmd, feeding each line to onLine, killing it if it stays
// silent for longer than the stall timeout. poll, when set, runs every
// pollEvery on this goroutine and reports progress steamcmd makes without
// printing; that progress also holds off the stall timeout.
func (s *SteamCMD) invoke(ctx context.Context, args []string, onLine func(string), poll func() bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	out, wait, err := s.run(ctx, args)
	if err != nil {
		return err
	}
	activity := make(chan struct{}, 1)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(splitCRLF)
		for sc.Scan() {
			select {
			case activity <- struct{}{}:
			default:
			}
			line := strings.TrimSpace(ansi.ReplaceAllString(sc.Text(), ""))
			if line == "" {
				continue
			}
			if s.o.OnLine != nil {
				s.o.OnLine(line)
			}
			if onLine != nil {
				onLine(line)
			}
		}
		io.Copy(io.Discard, out)
	}()
	timer := time.NewTimer(s.o.Stall)
	defer timer.Stop()
	var tick <-chan time.Time // nil without poll: never fires
	if poll != nil {
		t := time.NewTicker(s.pollEvery)
		defer t.Stop()
		tick = t.C
	}
loop:
	for {
		select {
		case <-scanDone:
			break loop
		case <-activity:
			timer.Reset(s.o.Stall)
		case <-tick:
			if poll() {
				timer.Reset(s.o.Stall)
			}
		case <-timer.C:
			cancel(ErrStalled)
			<-scanDone
			break loop
		}
	}
	werr := wait()
	if cause := context.Cause(ctx); errors.Is(cause, ErrStalled) {
		return ErrStalled
	}
	if werr != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		var ee *exec.ExitError
		// steamcmd commonly exits 7/8 after a successful run; callers judge by output.
		if errors.As(werr, &ee) {
			return nil
		}
		return fmt.Errorf("steamcmd: %w", werr)
	}
	return nil
}

func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

const tailLines = 8

var (
	reProgress = regexp.MustCompile(`Update state \((0x[0-9a-fA-F]+)\) ([a-z ,]+?), progress: ([0-9.]+)`)
	reAppOK    = regexp.MustCompile(`Success! App '(\d+)' (fully installed|already up to date)`)
	reAppErr   = regexp.MustCompile(`(?i)(Error! App '(\d+)' state is (0x[0-9a-fA-F]+) after update job|ERROR! Failed to install app '(\d+)')`)
	// One workshop event per match, in output order: steamcmd flushes late and
	// runs messages together ("…(1322250 bytes) Downloading item 3119788162 ...").
	reWsEvent    = regexp.MustCompile(`Downloading item (\d+) \.\.\.|Success\. Downloaded item (\d+) to|(?i:ERROR! Download item (\d+) failed \(([^)]*)\))`)
	reLoginError = regexp.MustCompile(`(?i)(FAILED \(|Login Failure|No subscription)`)
)

func phaseOf(state string) string {
	switch {
	case strings.Contains(state, "verifying"):
		return "verifying"
	case strings.Contains(state, "committing"):
		return "committing"
	case strings.Contains(state, "preallocating"):
		return "preallocating"
	default:
		return "downloading"
	}
}

func (s *SteamCMD) AppUpdate(ctx context.Context, branch string, validate bool, onProgress func(Progress)) error {
	if s.o.Guard != nil {
		if err := s.o.Guard(); err != nil {
			return err
		}
	}
	if onProgress == nil {
		onProgress = func(Progress) {}
	}
	args := []string{"+force_install_dir", s.o.InstallDir, "+login", "anonymous", "+app_update", AppID}
	if branch != "" && branch != "public" {
		args = append(args, "-beta", branch)
	}
	if validate {
		args = append(args, "validate")
	}
	args = append(args, "+quit")
	onProgress(Progress{Phase: "login", Message: "Connecting to Steam"})
	err := s.appUpdateOnce(ctx, args, onProgress)
	// A fresh Steam home has no cached app info, and steamcmd's first app_update
	// then often fails with "Missing configuration"; the rerun finds it cached.
	if err != nil && strings.Contains(err.Error(), "Missing configuration") {
		s.o.Log.Warn("steamcmd reported missing configuration, retrying once", "err", err)
		err = s.appUpdateOnce(ctx, args, onProgress)
	}
	if err != nil {
		return err
	}
	onProgress(Progress{Phase: "done", Percent: 100, Message: "Game files up to date"})
	return nil
}

func (s *SteamCMD) appUpdateOnce(ctx context.Context, args []string, onProgress func(Progress)) error {
	var ok bool
	var failure string
	var tail []string // last non-progress lines, for errors steamcmd reports in a form we don't recognise
	err := s.invoke(ctx, args, func(line string) {
		if m := reProgress.FindStringSubmatch(line); m != nil {
			pct, _ := strconv.ParseFloat(m[3], 64)
			onProgress(Progress{Phase: phaseOf(m[2]), Percent: pct, Message: line})
		} else {
			if len(tail) == tailLines {
				tail = tail[1:]
			}
			tail = append(tail, line)
		}
		if reAppOK.MatchString(line) {
			ok = true
		}
		if reAppErr.MatchString(line) || reLoginError.MatchString(line) {
			failure = line
		}
	}, nil)
	if err != nil {
		return err
	}
	if failure != "" {
		return fmt.Errorf("steamcmd: %s", failure)
	}
	if !ok {
		return fmt.Errorf("steamcmd: app_update finished without a success line; last output: %s", strings.Join(tail, " | "))
	}
	return nil
}

func (s *SteamCMD) LatestBuildID(ctx context.Context, branch string) (string, error) {
	var buf strings.Builder
	err := s.invoke(ctx, []string{"+login", "anonymous", "+app_info_update", "1", "+app_info_print", AppID, "+quit"},
		func(line string) { buf.WriteString(line); buf.WriteByte('\n') }, nil)
	if err != nil {
		return "", err
	}
	return ParseAppInfoBuildID(buf.String(), branch)
}

func (s *SteamCMD) InstalledBuild(context.Context) (AppManifest, error) {
	return ReadAppManifest(s.o.InstallDir)
}

// consoleEvery spaces the progress lines added to the console during a
// workshop download, which steamcmd itself leaves silent.
const consoleEvery = 10 * time.Second

func (s *SteamCMD) WorkshopDownload(ctx context.Context, items []WorkshopItem, onProgress func(Progress)) error {
	if len(items) == 0 {
		return nil
	}
	if onProgress == nil {
		onProgress = func(Progress) {}
	}
	args := []string{"+force_install_dir", s.o.InstallDir, "+login", "anonymous"}
	for _, it := range items {
		args = append(args, "+workshop_download_item", WorkshopAppID, it.ID)
	}
	args = append(args, "+quit")
	t := newWsTracker(items, onProgress, s.o.OnLine)
	onProgress(Progress{Phase: "login", Message: "Connecting to Steam"})
	err := s.invoke(ctx, args, func(line string) {
		for _, m := range reWsEvent.FindAllStringSubmatch(line, -1) {
			switch {
			case m[1] != "":
				t.start(m[1])
			case m[2] != "":
				t.finish(m[2])
			default:
				t.fail(m[3], m[4])
			}
		}
	}, func() bool { return t.measure(s.o.InstallDir) })
	if err != nil {
		return err
	}
	if missing := t.missing(); len(missing) > 0 {
		return fmt.Errorf("steamcmd: workshop download failed for %s", strings.Join(missing, ", "))
	}
	return nil
}

// wsTracker is one WorkshopDownload's state, shared by the output scanner and
// the staging-dir poll. It reports under its lock, so a measurement taken just
// before an item's success line cannot land after it.
type wsTracker struct {
	mu          sync.Mutex
	items       []WorkshopItem
	staged      []int64 // per item; never decreases: files leave the staging dir as the item completes
	done        []bool
	failed      map[string]string
	cur         int // the item steamcmd is on, -1 before the first
	onProgress  func(Progress)
	onLine      func(string) // console; nil for none
	lastConsole time.Time
}

func newWsTracker(items []WorkshopItem, onProgress func(Progress), onLine func(string)) *wsTracker {
	return &wsTracker{items: items, staged: make([]int64, len(items)), done: make([]bool, len(items)),
		failed: map[string]string{}, cur: -1, onProgress: onProgress, onLine: onLine}
}

func (t *wsTracker) index(id string) int {
	return slices.IndexFunc(t.items, func(it WorkshopItem) bool { return it.ID == id })
}

func (t *wsTracker) start(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i := t.index(id); i >= 0 {
		t.cur = i
		p, _ := t.progress("Downloading")
		t.onProgress(p)
	}
}

func (t *wsTracker) finish(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i := t.index(id); i >= 0 {
		t.cur, t.done[i] = i, true
		p, _ := t.progress("Downloaded")
		t.onProgress(p)
	}
}

func (t *wsTracker) fail(id, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed[id] = reason
}

// measure re-reads the staging dir of every item not done yet and reports
// whether any of them gained bytes, with a console line every consoleEvery.
func (t *wsTracker) measure(installDir string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	grew := false
	for i, it := range t.items {
		if t.done[i] {
			continue
		}
		if n := dirBytes(workshopStagingDir(installDir, it.ID)); n > t.staged[i] {
			t.staged[i], t.cur, grew = n, i, true
		}
	}
	if !grew {
		return false
	}
	p, console := t.progress("Downloading")
	t.onProgress(p)
	if t.onLine != nil && time.Since(t.lastConsole) >= consoleEvery {
		t.lastConsole = time.Now()
		t.onLine(console)
	}
	return true
}

// progress describes the current item; t.mu must be held. The percentage
// weighs items by size when every size is known, else counts them.
func (t *wsTracker) progress(verb string) (Progress, string) {
	bySize := !slices.ContainsFunc(t.items, func(it WorkshopItem) bool { return it.Size <= 0 })
	var done, total float64
	for i, it := range t.items {
		w := 1.0
		if bySize {
			w = float64(it.Size)
		}
		total += w
		switch {
		case t.done[i]:
			done += w
		case it.Size > 0:
			done += w * min(1, float64(t.staged[i])/float64(it.Size))
		}
	}
	it := t.items[t.cur]
	name := it.Title
	if name == "" {
		name = "item " + it.ID
	}
	msg := fmt.Sprintf("%s %s (%d/%d)", verb, name, t.cur+1, len(t.items))
	var console string
	if n := t.staged[t.cur]; n > 0 && !t.done[t.cur] {
		amount := formatBytes(n)
		if it.Size > 0 {
			amount += " of " + formatBytes(it.Size)
		}
		msg += ": " + amount
		console = "Downloading item " + it.ID + ": " + amount
		if it.Size > 0 {
			console += fmt.Sprintf(" (%.0f%%)", 100*min(1, float64(n)/float64(it.Size)))
		}
	}
	return Progress{Phase: "workshop", Percent: 100 * done / total, Message: msg}, console
}

// missing lists the items without a success line, with steamcmd's reason when it gave one.
func (t *wsTracker) missing() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for i, it := range t.items {
		if !t.done[i] {
			reason := t.failed[it.ID]
			if reason == "" {
				reason = "no success line"
			}
			out = append(out, it.ID+" ("+reason+")")
		}
	}
	return out
}

// dirBytes totals the regular files under dir, skipping whatever vanishes mid-walk.
func dirBytes(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error { // never fails: the callback returns nil
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// formatBytes matches the web UI's formatBytes, so a task's "of 257 MB" reads
// like the size on the mod's card.
func formatBytes(n int64) string {
	units := [...]string{"B", "KB", "MB", "GB", "TB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v >= 100 || i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func (s *SteamCMD) WorkshopInstalled(context.Context) (map[string]WorkshopItemState, error) {
	return ReadWorkshopManifest(s.o.InstallDir)
}

func (s *SteamCMD) WorkshopRemove(_ context.Context, id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return fmt.Errorf("steamcmd: invalid workshop id %q", id)
	}
	return os.RemoveAll(WorkshopContentDir(s.o.InstallDir, id))
}
