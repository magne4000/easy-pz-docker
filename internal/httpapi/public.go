package httpapi

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/pz/gamever"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/webui"
)

var statusMessages = map[string]string{
	"available":   "The server is up. Come on in.",
	"restarting":  "The server is restarting or updating. It will be back shortly.",
	"unavailable": "The server is currently offline.",
}

var wsidPath = regexp.MustCompile(`^[0-9]{1,20}$`)

func workshopURL(id string) string {
	return "https://steamcommunity.com/sharedfiles/filedetails/?id=" + id
}

// publicSet groups the enabled mods by workshop item, in load order.
type publicSet struct {
	all     []mods.ModInfo
	byItem  map[string][]mods.ModInfo
	order   []string
	updated map[string]time.Time
}

func newPublicSet(ms []mods.ModInfo, installed map[string]steam.WorkshopItemState) publicSet {
	ps := publicSet{all: ms, byItem: map[string][]mods.ModInfo{}, updated: map[string]time.Time{}}
	for _, m := range ms {
		if _, ok := ps.byItem[m.WorkshopID]; !ok {
			ps.order = append(ps.order, m.WorkshopID)
		}
		ps.byItem[m.WorkshopID] = append(ps.byItem[m.WorkshopID], m)
	}
	for id, st := range installed {
		ps.updated[id] = st.TimeUpdated
	}
	return ps
}

// publicConnect prefers the ini's DefaultPort, which the admin may have edited.
func publicConnect(d Deps) *publicapi.PublicConnect {
	if d.Cfg.PublicHost == "" {
		return nil
	}
	port := d.Cfg.DefaultPort
	if ini, err := pz.ReadIniFile(iniPath(d)); err == nil {
		if v, ok := ini.Get("DefaultPort"); ok {
			if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
				port = p
			}
		}
	}
	return &publicapi.PublicConnect{Host: d.Cfg.PublicHost, Port: port}
}

// jarVersion re-reads the jar only when it changes.
type jarVersion struct {
	mu      sync.Mutex
	path    string
	modTime time.Time
	size    int64
	version string
}

var serverGameVersion jarVersion

func (j *jarVersion) get(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.path == path && j.modTime.Equal(st.ModTime()) && j.size == st.Size() {
		return j.version
	}
	j.path, j.modTime, j.size, j.version = path, st.ModTime(), st.Size(), ""
	if v, err := gamever.FromJar(path); err == nil {
		j.version = v.String()
	}
	return j.version
}

func serverJar(d Deps) string {
	return filepath.Join(d.Cfg.InstallDir, "java", "projectzomboid.jar")
}

func packInfo(d Deps, key string, ms []mods.ModInfo, url string) publicapi.PublicPack {
	p, ready, err := d.Packer.Get(key, ms)
	out := publicapi.PublicPack{URL: url, Ready: ready, Size: p.Size, SHA256: p.SHA256}
	if err != nil {
		out.Error = "building the archive failed; it will be retried"
	}
	return out
}

func registerPublic(a *fiber.App, d Deps, log *slog.Logger) {
	guard := func(c fiber.Ctx) error {
		tok := c.Params("token")
		if !d.Settings.Get().PublicModsPage || subtle.ConstantTimeCompare([]byte(tok), []byte(d.Settings.ModsToken())) != 1 {
			return fiber.ErrNotFound
		}
		c.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		c.Set("Referrer-Policy", "no-referrer")
		return c.Next()
	}
	lim := func(max int) fiber.Handler {
		return limiter.New(limiter.Config{Max: max, Expiration: time.Minute, KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
			LimitReached: func(c fiber.Ctx) error { return problem(c, http.StatusTooManyRequests, "slow down") }})
	}
	g := a.Group("/mods/:token", guard)

	g.Get("/", lim(60), func(c fiber.Ctx) error {
		if !webui.Available() {
			return c.Status(http.StatusServiceUnavailable).SendString("the UI is not built into this binary")
		}
		return serveHTML("mods.html", nil)(c)
	})
	g.Get("/data.json", lim(60), func(c fiber.Ctx) error {
		ov, ms, installed, err := d.Mods.EnabledOverview(c.Context())
		if err != nil {
			return err
		}
		ps := newPublicSet(ms, installed)
		base := "/mods/" + c.Params("token") + "/"
		cfgName := d.Cfg.ServerName
		out := publicapi.PublicData{ServerName: cfgName, Status: d.Coord.Availability(), Items: []publicapi.PublicItem{}, IniName: cfgName + ".ini",
			GeneratedAt: time.Now().UTC()}
		out.StatusMessage = statusMessages[out.Status]
		out.Connect = publicConnect(d)
		out.GameVersion = serverGameVersion.get(serverJar(d))
		if out.Status == "available" {
			if p := d.Coord.Players(c.Context()); p.Count != nil {
				out.Players = p.Count
			}
		}
		if col := d.Settings.Get().WorkshopCollection; col != "" {
			out.Collection = &publicapi.PublicLink{ID: col, URL: workshopURL(col)}
		}
		titles := map[string]string{}
		sizes := map[string]int64{}
		for _, it := range ov.Items {
			titles[it.WorkshopID], sizes[it.WorkshopID] = it.Title, it.Size
		}
		out.ModsLine = strings.Join(ov.LoadOrder, ";")
		out.MapLine = strings.Join(ov.Maps, ";")
		keep := []string{}
		for _, id := range ps.order {
			ms := ps.byItem[id]
			key := mods.PackKey(ms, ps.updated)
			keep = append(keep, key)
			it := publicapi.PublicItem{WorkshopID: id, Title: titles[id], URL: workshopURL(id), Size: sizes[id], TimeUpdated: ps.updated[id], Mods: []publicapi.PublicMod{},
				Download: packInfo(d, key, ms, base+"download/"+id+".zip")}
			for _, m := range ms {
				it.Mods = append(it.Mods, publicapi.PublicMod{ID: m.ID, Name: m.Name, Folder: m.FolderName})
			}
			out.Items = append(out.Items, it)
		}
		if len(ps.all) > 0 {
			key := mods.PackKey(ps.all, ps.updated)
			keep = append(keep, key)
			out.Pack = packInfo(d, key, ps.all, base+"download/all.zip")
		}
		d.Packer.Prune(keep...)
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.JSON(out)
	})
	g.Get("/download/:file", lim(20), func(c fiber.Ctx) error {
		name := strings.TrimSuffix(c.Params("file"), ".zip")
		if name == c.Params("file") {
			return fiber.ErrNotFound
		}
		enabled, installed, err := d.Mods.Enabled(c.Context())
		if err != nil {
			return err
		}
		ps := newPublicSet(enabled, installed)
		ms := ps.all
		filename := fmt.Sprintf("%s-mods.zip", d.Cfg.ServerName)
		if name != "all" {
			if !wsidPath.MatchString(name) || len(ps.byItem[name]) == 0 {
				return fiber.ErrNotFound
			}
			ms = ps.byItem[name]
			filename = fmt.Sprintf("%s-%s.zip", d.Cfg.ServerName, name)
		}
		if len(ms) == 0 {
			return fiber.ErrNotFound
		}
		p, ready, err := d.Packer.Get(mods.PackKey(ms, ps.updated), ms)
		if err != nil {
			log.Error("public mod pack", "err", err)
			return fiber.NewError(http.StatusInternalServerError, "building the archive failed; retry in a minute")
		}
		if !ready {
			c.Set(fiber.HeaderRetryAfter, "15")
			return problem(c, http.StatusServiceUnavailable, "the archive is being prepared, retry shortly")
		}
		c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(filename)))
		c.Set("X-Checksum-SHA256", p.SHA256)
		return c.SendFile(p.Path, fiber.SendFile{ByteRange: true})
	})
}
