package steam

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
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
	WorkshopDownload(ctx context.Context, ids []string, onProgress func(Progress)) error
	WorkshopInstalled(ctx context.Context) (map[string]WorkshopItemState, error)
	WorkshopRemove(ctx context.Context, id string) error
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
	o   Options
	mu  sync.Mutex // one steamcmd at a time: they share a lock file
	run runFunc
}

func NewSteamCMD(o Options) *SteamCMD {
	if o.Stall <= 0 {
		o.Stall = 10 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	s := &SteamCMD{o: o}
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

var ErrStalled = errors.New("steamcmd: no output within the stall timeout, killed")

// invoke runs steamcmd, feeding each line to onLine, killing it if it stays
// silent for longer than the stall timeout.
func (s *SteamCMD) invoke(ctx context.Context, args []string, onLine func(string)) error {
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
loop:
	for {
		select {
		case <-scanDone:
			break loop
		case <-activity:
			timer.Reset(s.o.Stall)
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

var (
	reProgress   = regexp.MustCompile(`Update state \((0x[0-9a-fA-F]+)\) ([a-z ,]+?), progress: ([0-9.]+)`)
	reAppOK      = regexp.MustCompile(`Success! App '(\d+)' (fully installed|already up to date)`)
	reAppErr     = regexp.MustCompile(`(?i)Error! App '(\d+)' state is (0x[0-9a-fA-F]+) after update job`)
	reWsOK       = regexp.MustCompile(`Success\. Downloaded item (\d+) to`)
	reWsErr      = regexp.MustCompile(`(?i)ERROR! Download item (\d+) failed \(([^)]*)\)`)
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
	var ok bool
	var failure string
	err := s.invoke(ctx, args, func(line string) {
		if m := reProgress.FindStringSubmatch(line); m != nil {
			pct, _ := strconv.ParseFloat(m[3], 64)
			onProgress(Progress{Phase: phaseOf(m[2]), Percent: pct, Message: line})
		}
		if reAppOK.MatchString(line) {
			ok = true
		}
		if reAppErr.MatchString(line) || reLoginError.MatchString(line) {
			failure = line
		}
	})
	if err != nil {
		return err
	}
	if failure != "" {
		return fmt.Errorf("steamcmd: %s", failure)
	}
	if !ok {
		return errors.New("steamcmd: app_update finished without a success line")
	}
	onProgress(Progress{Phase: "done", Percent: 100, Message: "Game files up to date"})
	return nil
}

func (s *SteamCMD) LatestBuildID(ctx context.Context, branch string) (string, error) {
	var buf strings.Builder
	err := s.invoke(ctx, []string{"+login", "anonymous", "+app_info_update", "1", "+app_info_print", AppID, "+quit"},
		func(line string) { buf.WriteString(line); buf.WriteByte('\n') })
	if err != nil {
		return "", err
	}
	return ParseAppInfoBuildID(buf.String(), branch)
}

func (s *SteamCMD) InstalledBuild(context.Context) (AppManifest, error) {
	return ReadAppManifest(s.o.InstallDir)
}

func (s *SteamCMD) WorkshopDownload(ctx context.Context, ids []string, onProgress func(Progress)) error {
	if len(ids) == 0 {
		return nil
	}
	if onProgress == nil {
		onProgress = func(Progress) {}
	}
	args := []string{"+force_install_dir", s.o.InstallDir, "+login", "anonymous"}
	for _, id := range ids {
		args = append(args, "+workshop_download_item", WorkshopAppID, id)
	}
	args = append(args, "+quit")
	done := map[string]bool{}
	failed := map[string]string{}
	err := s.invoke(ctx, args, func(line string) {
		if m := reWsOK.FindStringSubmatch(line); m != nil {
			done[m[1]] = true
			onProgress(Progress{Phase: "workshop", Percent: 100 * float64(len(done)) / float64(len(ids)), Message: "Downloaded " + m[1]})
		}
		if m := reWsErr.FindStringSubmatch(line); m != nil {
			failed[m[1]] = m[2]
		}
	})
	if err != nil {
		return err
	}
	var missing []string
	for _, id := range ids {
		if !done[id] {
			reason := failed[id]
			if reason == "" {
				reason = "no success line"
			}
			missing = append(missing, id+" ("+reason+")")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("steamcmd: workshop download failed for %s", strings.Join(missing, ", "))
	}
	return nil
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
