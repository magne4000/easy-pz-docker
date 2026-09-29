package httpapi

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/webui"
)

// PublicData feeds the unauthenticated mod page, reachable only through the
// unlisted token in its URL.
type PublicData struct {
	ServerName    string       `json:"serverName"`
	Status        string       `json:"status" enum:"available,restarting,unavailable"`
	StatusMessage string       `json:"statusMessage"`
	Players       *int         `json:"players"`
	Collection    *PublicLink  `json:"collection,omitempty"`
	Items         []PublicItem `json:"items" nullable:"false"`
	ModsLine      string       `json:"modsLine"`
	MapLine       string       `json:"mapLine"`
	IniName       string       `json:"iniName"`
	Pack          PublicPack   `json:"pack"`
	GeneratedAt   time.Time    `json:"generatedAt"`
}

type PublicLink struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type PublicMod struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

type PublicItem struct {
	WorkshopID  string      `json:"workshopId"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	Size        int64       `json:"size"`
	TimeUpdated time.Time   `json:"timeUpdated,omitzero"`
	Mods        []PublicMod `json:"mods" nullable:"false"`
	Download    PublicPack  `json:"download"`
}

type PublicPack struct {
	URL    string `json:"url"`
	Ready  bool   `json:"ready"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Error  string `json:"error,omitempty"`
}

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

func packInfo(d Deps, key string, ms []mods.ModInfo, url string) PublicPack {
	p, ready, err := d.Packer.Get(key, ms)
	out := PublicPack{URL: url, Ready: ready, Size: p.Size, SHA256: p.SHA256}
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
		out := PublicData{ServerName: cfgName, Status: d.Coord.Availability(), Items: []PublicItem{}, IniName: cfgName + ".ini",
			GeneratedAt: time.Now().UTC()}
		out.StatusMessage = statusMessages[out.Status]
		if out.Status == "available" {
			if p := d.Coord.Players(c.Context()); p.Count != nil {
				out.Players = p.Count
			}
		}
		if col := d.Settings.Get().WorkshopCollection; col != "" {
			out.Collection = &PublicLink{ID: col, URL: workshopURL(col)}
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
			it := PublicItem{WorkshopID: id, Title: titles[id], URL: workshopURL(id), Size: sizes[id], TimeUpdated: ps.updated[id], Mods: []PublicMod{},
				Download: packInfo(d, key, ms, base+"download/"+id+".zip")}
			for _, m := range ms {
				it.Mods = append(it.Mods, PublicMod{ID: m.ID, Name: m.Name, Folder: m.FolderName})
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
