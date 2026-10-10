package mods

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

const DefaultMap = "Muldraugh, KY"

var wsidRe = regexp.MustCompile(`^[0-9]{1,20}$`)

var ErrInvalidID = errors.New("workshop ids are numeric")

type Options struct {
	InstallDir string
	DataDir    string
	ServerName string
	NonSteam   bool
	DB         *store.DB
	CMD        steam.CMD
	WebAPI     steam.WebAPI
	Bus        *events.Bus
	Tasks      *tasks.Registry
	Log        *slog.Logger
	// Loaded returns the Mods= list the running server was started with (nil when stopped).
	Loaded func() []string
	// Collection returns the configured workshop collection id.
	Collection func() string
}

type Service struct {
	o     Options
	mu    sync.Mutex // serialises mutations of links + ini
	scanM sync.Mutex
	scans map[string]scanResult
}

type scanResult struct {
	mods []ModInfo
	err  error
}

func NewService(o Options) *Service {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{o: o, scans: map[string]scanResult{}}
}

func (s *Service) IniPath() string {
	return filepath.Join(s.o.DataDir, "Server", s.o.ServerName+".ini")
}

func (s *Service) ModsDir() string { return filepath.Join(s.o.DataDir, "mods") }

func (s *Service) contentDir(id string) string { return steam.WorkshopContentDir(s.o.InstallDir, id) }

func (s *Service) scan(id string) ([]ModInfo, error) {
	s.scanM.Lock()
	defer s.scanM.Unlock()
	if r, ok := s.scans[id]; ok {
		return r.mods, r.err
	}
	ms, err := ScanWorkshopItem(s.contentDir(id), id)
	if errors.Is(err, fs.ErrNotExist) {
		ms, err = nil, nil
	}
	s.scans[id] = scanResult{ms, err}
	return ms, err
}

func (s *Service) invalidate(ids ...string) {
	s.scanM.Lock()
	defer s.scanM.Unlock()
	if len(ids) == 0 {
		s.scans = map[string]scanResult{}
		return
	}
	for _, id := range ids {
		delete(s.scans, id)
	}
}

func (s *Service) changed() { s.o.Bus.Publish(events.ModsChanged{}) }

type ModView struct {
	ModID    string   `json:"modId"`
	Name     string   `json:"name"`
	Folder   string   `json:"folder"`
	Enabled  bool     `json:"enabled"`
	Requires []string `json:"requires"`
	Missing  []string `json:"missing" doc:"requirements not provided by any enabled mod"`
	Maps     []string `json:"maps"`
}

type ItemView struct {
	WorkshopID       string    `json:"workshopId"`
	Title            string    `json:"title"`
	Installed        bool      `json:"installed"`
	InstalledUpdated time.Time `json:"installedUpdated,omitzero"`
	RemoteUpdated    time.Time `json:"remoteUpdated,omitzero"`
	UpdateAvailable  bool      `json:"updateAvailable"`
	Size             int64     `json:"size"`
	PreviewURL       string    `json:"previewUrl"`
	CheckedAt        time.Time `json:"checkedAt,omitzero"`
	Mods             []ModView `json:"mods"`
	Error            string    `json:"error,omitempty"`
}

type Overview struct {
	Items           []ItemView `json:"items"`
	LoadOrder       []string   `json:"loadOrder" doc:"Mods= as it will be written"`
	Maps            []string   `json:"maps" doc:"Map= as it will be written"`
	Cycles          [][]string `json:"cycles"`
	IniMods         []string   `json:"iniMods" doc:"Mods= currently in the ini"`
	LoadedMods      []string   `json:"loadedMods" doc:"Mods= the running server was started with"`
	RestartRequired bool       `json:"restartRequired"`
	NonSteam        bool       `json:"nonSteam"`
	Collection      string     `json:"collection"`
}

// state is the desired mod set computed from the DB and the downloaded files.
type state struct {
	items     []store.TrackedItem
	installed map[string]steam.WorkshopItemState
	scanned   map[string][]ModInfo
	scanErr   map[string]error
	entries   map[string]store.ModEntry // key wsid/modid; a scanned mod may have none yet
	ordered   []ModInfo                 // enabled mods in load order
	cycles    [][]string
}

// enabled reports whether a scanned mod is enabled. A mod with no entry yet
// (just downloaded, or fetched by PZ itself in Steam mode) is enabled.
func (st *state) enabled(wsid, modID string) bool {
	e, ok := st.entries[entryKey(wsid, modID)]
	return !ok || e.Enabled
}

func entryKey(wsid, modID string) string { return wsid + "/" + modID }

func (s *Service) compute(ctx context.Context) (*state, error) {
	items, err := s.o.DB.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := s.o.CMD.WorkshopInstalled(ctx)
	if err != nil {
		return nil, err
	}
	st := &state{items: items, installed: installed, scanned: map[string][]ModInfo{}, scanErr: map[string]error{}, entries: map[string]store.ModEntry{}}
	for _, it := range items {
		st.scanned[it.WorkshopID], st.scanErr[it.WorkshopID] = s.scan(it.WorkshopID)
	}
	entries, err := s.o.DB.ListModEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		st.entries[entryKey(e.WorkshopID, e.ModID)] = e
	}
	// Mods without an entry sort last, in discovery order.
	pos := func(m ModInfo) int {
		if e, ok := st.entries[entryKey(m.WorkshopID, m.ID)]; ok {
			return e.Position
		}
		return math.MaxInt
	}
	var enabled []ModInfo
	seen := map[string]bool{}
	for _, it := range items {
		for _, m := range st.scanned[it.WorkshopID] {
			if st.enabled(it.WorkshopID, m.ID) && !seen[m.ID] {
				seen[m.ID] = true
				enabled = append(enabled, m)
			}
		}
	}
	slices.SortStableFunc(enabled, func(a, b ModInfo) int { return cmp.Compare(pos(a), pos(b)) })
	st.ordered, _, st.cycles = SortLoadOrder(enabled)
	return st, nil
}

// persistEntries records every scanned mod that has no entry yet, in
// discovery order, so an explicit choice (enable/disable, order) has a row to
// land on. Only write paths call it; compute stays read-only.
func (s *Service) persistEntries(ctx context.Context, st *state) error {
	for _, it := range st.items {
		for _, m := range st.scanned[it.WorkshopID] {
			if _, ok := st.entries[entryKey(it.WorkshopID, m.ID)]; ok {
				continue
			}
			if err := s.o.DB.EnsureModEntry(ctx, it.WorkshopID, m.ID, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (st *state) modIDs() []string {
	out := make([]string, 0, len(st.ordered))
	for _, m := range st.ordered {
		out = append(out, m.ID)
	}
	return out
}

func (st *state) workshopIDs() []string {
	var out []string
	for _, it := range st.items {
		for _, m := range st.ordered {
			if m.WorkshopID == it.WorkshopID {
				out = append(out, it.WorkshopID)
				break
			}
		}
	}
	return out
}

// maps computes Map=: maps of enabled mods first, then the ini's other entries.
func (st *state) maps(current []string) []string {
	provided := map[string]bool{}
	for _, ms := range st.scanned {
		for _, m := range ms {
			for _, mp := range m.Maps {
				provided[mp] = true
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, m := range st.ordered {
		for _, mp := range m.Maps {
			add(mp)
		}
	}
	for _, c := range current {
		if !provided[c] {
			add(c)
		}
	}
	if len(current) == 0 || len(out) == 0 {
		add(DefaultMap)
	}
	return out
}

func (s *Service) readIni() (*pz.Ini, error) {
	d, err := pz.ReadIniFile(s.IniPath())
	if errors.Is(err, fs.ErrNotExist) {
		return pz.NewIni(), nil
	}
	return d, err
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	st, err := s.compute(ctx)
	if err != nil {
		return Overview{}, err
	}
	return s.overview(st)
}

// EnabledOverview is Overview plus Enabled from a single computation.
func (s *Service) EnabledOverview(ctx context.Context) (Overview, []ModInfo, map[string]steam.WorkshopItemState, error) {
	st, err := s.compute(ctx)
	if err != nil {
		return Overview{}, nil, nil, err
	}
	ov, err := s.overview(st)
	return ov, st.ordered, st.installed, err
}

func (s *Service) overview(st *state) (Overview, error) {
	ini, err := s.readIni()
	if err != nil {
		return Overview{}, err
	}
	enabled := map[string]bool{}
	for _, m := range st.ordered {
		enabled[m.ID] = true
	}
	ov := Overview{Items: []ItemView{}, LoadOrder: st.modIDs(), Maps: st.maps(ini.GetList("Map")), Cycles: st.cycles,
		IniMods: ini.GetList("Mods"), NonSteam: s.o.NonSteam}
	if ov.Cycles == nil {
		ov.Cycles = [][]string{}
	}
	if s.o.Collection != nil {
		ov.Collection = s.o.Collection()
	}
	if s.o.Loaded != nil {
		ov.LoadedMods = s.o.Loaded()
	}
	if ov.LoadedMods != nil {
		ov.RestartRequired = !slices.Equal(ov.LoadedMods, ov.LoadOrder)
	} else {
		ov.LoadedMods = []string{}
	}
	if ov.IniMods == nil {
		ov.IniMods = []string{}
	}
	for _, it := range st.items {
		inst, ok := st.installed[it.WorkshopID]
		v := ItemView{WorkshopID: it.WorkshopID, Title: it.Title, RemoteUpdated: it.RemoteUpdated, Size: it.FileSize,
			PreviewURL: it.PreviewURL, CheckedAt: it.CheckedAt, Mods: []ModView{}}
		if ok {
			v.InstalledUpdated = inst.TimeUpdated
			if v.Size == 0 {
				v.Size = inst.Size
			}
		}
		v.Installed = ok && len(st.scanned[it.WorkshopID]) > 0
		v.UpdateAvailable = !v.Installed || (!it.RemoteUpdated.IsZero() && it.RemoteUpdated.After(inst.TimeUpdated))
		if err := st.scanErr[it.WorkshopID]; err != nil {
			v.Error = err.Error()
		} else if ok && len(st.scanned[it.WorkshopID]) == 0 {
			v.Error = "downloaded item contains no mods (no mods/*/mod.info)"
		}
		if v.Title == "" {
			v.Title = it.WorkshopID
		}
		for _, m := range st.scanned[it.WorkshopID] {
			mv := ModView{ModID: m.ID, Name: m.Name, Folder: m.FolderName, Enabled: st.enabled(it.WorkshopID, m.ID), Requires: nonNil(m.Requires), Maps: nonNil(m.Maps), Missing: []string{}}
			for _, r := range m.Requires {
				if !enabled[r] {
					mv.Missing = append(mv.Missing, r)
				}
			}
			v.Mods = append(v.Mods, mv)
		}
		ov.Items = append(ov.Items, v)
	}
	return ov, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Apply writes Mods=/WorkshopItems=/Map= and reconciles the mod links.
// It never touches the ini's other keys.
func (s *Service) Apply(ctx context.Context) (LinkResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.compute(ctx)
	if err != nil {
		return LinkResult{}, err
	}
	desired := map[string]string{}
	if s.o.NonSteam {
		for _, it := range st.items {
			for _, m := range st.scanned[it.WorkshopID] {
				if _, dup := desired[m.FolderName]; !dup {
					desired[m.FolderName] = m.Dir
				}
			}
		}
	}
	res, err := ReconcileLinks(s.ModsDir(), steam.WorkshopContentRoot(s.o.InstallDir), desired)
	if err != nil {
		return res, err
	}
	for _, e := range res.Errors {
		s.o.Log.Warn("mod link", "error", e)
	}
	ini, err := s.readIni()
	if err != nil {
		return res, err
	}
	before := string(ini.Bytes())
	ini.SetList("Mods", st.modIDs())
	ini.SetList("WorkshopItems", st.workshopIDs())
	ini.SetList("Map", st.maps(ini.GetList("Map")))
	if string(ini.Bytes()) != before {
		if err := os.MkdirAll(filepath.Dir(s.IniPath()), 0o755); err != nil {
			return res, err
		}
		if err := pz.WriteIniFileAtomic(s.IniPath(), ini, 0o644); err != nil {
			return res, err
		}
		s.o.Bus.Publish(events.ConfigChanged{})
	}
	s.changed()
	return res, nil
}

// Verify is the boot self-check over every linked mod.
func (s *Service) Verify(ctx context.Context) error {
	if !s.o.NonSteam {
		return nil
	}
	st, err := s.compute(ctx)
	if err != nil {
		return err
	}
	var names []string
	var errs []error
	for _, m := range st.ordered {
		names = append(names, m.FolderName)
	}
	for _, it := range st.items {
		if len(st.scanned[it.WorkshopID]) == 0 {
			errs = append(errs, fmt.Errorf("workshop item %s is tracked but not downloaded", it.WorkshopID))
		}
	}
	if err := VerifyLinks(s.ModsDir(), names); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func validIDs(ids []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !wsidRe.MatchString(id) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidID, id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// Add tracks items, fetches their metadata, downloads the new ones and applies.
// New downloads are new directories, so this is safe while the server runs;
// the mods load at the next restart.
func (s *Service) Add(ctx context.Context, ids []string) ([]string, error) {
	ids, err := validIDs(ids)
	if err != nil {
		return nil, err
	}
	added := []string{}
	for _, id := range ids {
		ok, err := s.o.DB.AddItem(ctx, id, time.Now())
		if err != nil {
			return added, err
		}
		if ok {
			added = append(added, id)
		}
	}
	if len(added) == 0 {
		return added, nil
	}
	s.changed()
	if err := s.refreshDetails(ctx, added); err != nil {
		s.o.Log.Warn("steam web api", "err", err)
	}
	h := s.o.Tasks.Start("workshop", fmt.Sprintf("Downloading %d workshop item(s)", len(added)))
	go func() {
		// Never carry the request context into a goroutine: Fiber recycles it.
		ctx := context.Background()
		err := s.download(ctx, added, h)
		if err == nil {
			_, err = s.Apply(ctx)
		}
		h.Finish(err)
		if err != nil {
			s.o.Log.Error("workshop download", "err", err)
		}
		s.changed()
	}()
	return added, nil
}

func (s *Service) download(ctx context.Context, ids []string, h *tasks.Handle) error {
	defer s.invalidate(ids...)
	tracked, err := s.o.DB.ListItems(ctx)
	if err != nil {
		return err
	}
	items := make([]steam.WorkshopItem, len(ids))
	at := make(map[string]int, len(ids))
	for i, id := range ids {
		items[i].ID, at[id] = id, i
	}
	for _, it := range tracked {
		if i, ok := at[it.WorkshopID]; ok {
			items[i].Title, items[i].Size = it.Title, it.FileSize
		}
	}
	return s.o.CMD.WorkshopDownload(ctx, items, func(p steam.Progress) {
		if h != nil {
			h.Progress(p.Percent, p.Message)
		}
	})
}

// Remove untracks an item, unlinks it, rewrites the ini and deletes the
// cached download.
func (s *Service) Remove(ctx context.Context, id string) error {
	if err := s.o.DB.RemoveItem(ctx, id); err != nil {
		return err
	}
	s.invalidate(id)
	if _, err := s.Apply(ctx); err != nil {
		return err
	}
	if err := s.o.CMD.WorkshopRemove(ctx, id); err != nil {
		return err
	}
	s.invalidate(id)
	s.changed()
	return nil
}

func (s *Service) SetEnabled(ctx context.Context, wsid, modID string, enabled bool) error {
	if err := s.withEntries(ctx, func() error { return s.o.DB.SetModEnabled(ctx, wsid, modID, enabled) }); err != nil {
		return err
	}
	_, err := s.Apply(ctx)
	return err
}

func (s *Service) SetOrder(ctx context.Context, modIDs []string) error {
	if err := s.withEntries(ctx, func() error { return s.o.DB.SetModOrder(ctx, modIDs) }); err != nil {
		return err
	}
	_, err := s.Apply(ctx)
	return err
}

// withEntries runs a mod-entry write once every scanned mod has a row.
func (s *Service) withEntries(ctx context.Context, write func() error) error {
	st, err := s.compute(ctx)
	if err != nil {
		return err
	}
	if err := s.persistEntries(ctx, st); err != nil {
		return err
	}
	return write()
}

// AutoSort persists the dependency-respecting order as the manual order.
func (s *Service) AutoSort(ctx context.Context) error {
	st, err := s.compute(ctx)
	if err != nil {
		return err
	}
	return s.SetOrder(ctx, st.modIDs())
}

func (s *Service) refreshDetails(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ds, err := s.o.WebAPI.PublishedFileDetails(ctx, ids)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, d := range ds {
		if d.Result != 1 {
			continue
		}
		if err := s.o.DB.UpdateItemRemote(ctx, d.ID, d.Title, d.TimeUpdated, d.FileSize, d.PreviewURL, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) trackedIDs(ctx context.Context) ([]string, error) {
	items, err := s.o.DB.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.WorkshopID)
	}
	return ids, nil
}

// CheckUpdates refreshes Steam metadata and returns the tracked items whose
// download is missing or older than the Workshop's. Relevance is scoped to
// this server's own tracked list, never the shared ACF (upstream's phantom updates).
func (s *Service) CheckUpdates(ctx context.Context) ([]string, error) {
	ids, err := s.trackedIDs(ctx)
	if err != nil {
		return nil, err
	}
	werr := s.refreshDetails(ctx, ids)
	ov, err := s.Overview(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, it := range ov.Items {
		if it.UpdateAvailable {
			out = append(out, it.WorkshopID)
		}
	}
	s.changed()
	return out, werr
}

// DownloadOutdated fetches every missing or outdated item. Updating rewrites
// files a live server has loaded, so it only runs with the server stopped:
// inside the update window or before the first start.
func (s *Service) DownloadOutdated(ctx context.Context, h *tasks.Handle) ([]string, error) {
	if s.o.Loaded != nil && s.o.Loaded() != nil {
		return nil, errors.New("refusing to update workshop downloads while the game server is running")
	}
	ids, err := s.pick(ctx, func(it ItemView) bool { return it.UpdateAvailable })
	if err != nil || len(ids) == 0 {
		return ids, err
	}
	return ids, s.download(ctx, ids, h)
}

// DownloadMissing fetches tracked items that have no usable download yet (a
// mod added while its background download failed, or never ran). It runs
// before every non-Steam start so the boot self-check fails only when the
// download genuinely fails. Outdated items wait for the update window;
// in Steam mode PZ fetches WorkshopItems= itself.
func (s *Service) DownloadMissing(ctx context.Context) ([]string, error) {
	if !s.o.NonSteam {
		return nil, nil
	}
	s.invalidate() // downloads may have changed on disk since the last scan
	ids, err := s.pick(ctx, func(it ItemView) bool { return !it.Installed })
	if err != nil || len(ids) == 0 {
		return ids, err
	}
	h := s.o.Tasks.Start("workshop", fmt.Sprintf("Downloading %d missing workshop item(s)", len(ids)))
	err = s.download(ctx, ids, h)
	h.Finish(err)
	return ids, err
}

func (s *Service) pick(ctx context.Context, want func(ItemView) bool) ([]string, error) {
	ov, err := s.Overview(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, it := range ov.Items {
		if want(it) {
			ids = append(ids, it.WorkshopID)
		}
	}
	return ids, nil
}

func (s *Service) ImportCollection(ctx context.Context, collectionID string) ([]string, error) {
	if !wsidRe.MatchString(collectionID) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidID, collectionID)
	}
	ids, err := s.o.WebAPI.CollectionDetails(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	return s.Add(ctx, ids)
}

func (s *Service) Conflicts(ctx context.Context) ([]Conflict, error) {
	st, err := s.compute(ctx)
	if err != nil {
		return nil, err
	}
	return FindConflicts(st.ordered)
}

// Enabled returns the enabled mods in load order, with their workshop item state.
func (s *Service) Enabled(ctx context.Context) ([]ModInfo, map[string]steam.WorkshopItemState, error) {
	st, err := s.compute(ctx)
	if err != nil {
		return nil, nil, err
	}
	return st.ordered, st.installed, nil
}

// DesiredMods is Mods= as Apply writes it.
func (s *Service) DesiredMods(ctx context.Context) ([]string, error) {
	st, err := s.compute(ctx)
	if err != nil {
		return nil, err
	}
	return st.modIDs(), nil
}

// Refresh drops cached scans (after an external download).
func (s *Service) Refresh() { s.invalidate() }
