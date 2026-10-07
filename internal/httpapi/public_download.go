package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"

	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

// downloadSlots caps the transfers one address runs at once. A request rate
// limit cannot bound bandwidth (all.zip is one request) and starves the
// launcher, which fetches one pack per workshop item.
const downloadSlots = 4

// slots counts in-flight transfers per client address. A slot is held until
// the response body is written or abandoned, not just until the handler
// returns: fasthttp writes streamed bodies afterwards.
type slots struct {
	mu  sync.Mutex
	max int
	n   map[string]int
}

func newSlots(max int) *slots { return &slots{max: max, n: map[string]int{}} }

type slotKey struct{}

func (s *slots) acquire(c fiber.Ctx) error {
	ip := strings.Clone(c.IP()) // outlives the request
	s.mu.Lock()
	if s.n[ip] >= s.max {
		s.mu.Unlock()
		c.Set(fiber.HeaderRetryAfter, "5")
		return problem(c, http.StatusTooManyRequests, "too many downloads at once from your address; retry shortly")
	}
	s.n[ip]++
	s.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			if s.n[ip]--; s.n[ip] <= 0 {
				delete(s.n, ip)
			}
			s.mu.Unlock()
		})
	}
	c.Locals(slotKey{}, release)
	err := c.Next()
	// A streamed body releases the slot when fasthttp closes it, including
	// when an error response replaces it.
	if !c.Response().IsBodyStream() {
		release()
	}
	return err
}

type slotBody struct {
	io.Reader
	closer  io.Closer
	release func()
}

func (b *slotBody) Close() error {
	if b.release != nil {
		defer b.release()
	}
	return b.closer.Close()
}

// streamBody hands r to fasthttp, which closes it once the response is written
// or abandoned; closer and the download slot are released then. size < 0
// sends it chunked.
func streamBody(c fiber.Ctx, r io.Reader, size int64, closer io.Closer) error {
	release, _ := c.Locals(slotKey{}).(func())
	c.Response().SetBodyStream(&slotBody{Reader: r, closer: closer, release: release}, int(size))
	return nil
}

// sendFile serves path with single-range support (resumed downloads). It
// replaces c.SendFile, whose body reader cannot hold the download slot.
func sendFile(c fiber.Ctx, path, etag, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	size := st.Size()
	start, n := int64(0), size
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderAcceptRanges, "bytes")
	c.Set(fiber.HeaderETag, etag)
	if spec := c.Get(fiber.HeaderRange); spec != "" && (c.Get(fiber.HeaderIfRange) == "" || c.Get(fiber.HeaderIfRange) == etag) {
		s, l, ok := parseRange(spec, size)
		switch {
		case !ok: // malformed or multiple ranges: the whole file, as RFC 9110 allows
		case l == 0:
			f.Close()
			c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes */%d", size))
			return c.SendStatus(http.StatusRequestedRangeNotSatisfiable)
		default:
			start, n = s, l
			c.Status(http.StatusPartialContent)
			c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes %d-%d/%d", s, s+l-1, size))
		}
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return err
	}
	return streamBody(c, io.LimitReader(f, n), n, f)
}

// parseRange reads one "bytes=" range of a size-byte file. ok=false: ignore
// the header; length 0: unsatisfiable.
func parseRange(spec string, size int64) (start, length int64, ok bool) {
	r, found := strings.CutPrefix(spec, "bytes=")
	if !found || strings.Contains(r, ",") {
		return 0, 0, false
	}
	a, b, found := strings.Cut(strings.TrimSpace(r), "-")
	if !found {
		return 0, 0, false
	}
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		if n = min(n, size); n == 0 {
			return 0, 0, true
		}
		return size - n, n, true
	}
	s, err := strconv.ParseInt(a, 10, 64)
	if err != nil || s < 0 {
		return 0, 0, false
	}
	end := size - 1
	if b != "" {
		e, err := strconv.ParseInt(b, 10, 64)
		if err != nil || e < s {
			return 0, 0, false
		}
		end = min(e, end)
	}
	if s >= size {
		return 0, 0, true
	}
	return s, end - s + 1, true
}

// publicPack resolves "all" or a workshop id to its pack.
func publicPack(ctx context.Context, d Deps, log *slog.Logger, name string) (mods.Pack, bool, error) {
	enabled, installed, err := d.Mods.Enabled(ctx)
	if err != nil {
		return mods.Pack{}, false, err
	}
	ps := newPublicSet(enabled, installed)
	ms := ps.all
	if name != "all" {
		if !wsidPath.MatchString(name) {
			return mods.Pack{}, false, fiber.ErrNotFound
		}
		ms = ps.byItem[name]
	}
	if len(ms) == 0 {
		return mods.Pack{}, false, fiber.ErrNotFound
	}
	p, ready, err := d.Packer.Get(mods.PackKey(ms, ps.updated), ms)
	if err != nil {
		log.Error("public mod pack", "err", err)
		return mods.Pack{}, false, fiber.NewError(http.StatusInternalServerError, "building the archive failed; retry in a minute")
	}
	return p, ready, nil
}

func notReady(c fiber.Ctx) error {
	c.Set(fiber.HeaderRetryAfter, "15")
	return problem(c, http.StatusServiceUnavailable, "the archive is being prepared, retry shortly")
}

func etag(p mods.Pack) string { return `"` + p.SHA256 + `"` }

// pickFiles maps listing indexes to pack entry names, in listing order.
func pickFiles(listing string, idx []int) ([]string, error) {
	if len(idx) == 0 {
		return nil, fiber.NewError(http.StatusBadRequest, "no files requested")
	}
	b, err := os.ReadFile(listing)
	if err != nil {
		return nil, err
	}
	var l publicapi.PackFiles
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	want := make([]bool, len(l.Files))
	for _, i := range idx {
		if i < 0 || i >= len(l.Files) {
			return nil, fiber.NewError(http.StatusBadRequest, fmt.Sprintf("file %d is not in the listing", i))
		}
		want[i] = true
	}
	var names []string
	for i, f := range l.Files {
		if want[i] {
			names = append(names, f.Path)
		}
	}
	return names, nil
}

// writeFiles copies the named pack entries into a new zip without
// recompressing them.
func writeFiles(w io.Writer, zr *zip.Reader, names []string) error {
	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	zw := zip.NewWriter(w)
	for _, name := range names {
		f := byName[name]
		if f == nil {
			return fmt.Errorf("%s is not in the pack", name)
		}
		fh := f.FileHeader
		dst, err := zw.CreateRaw(&fh)
		if err != nil {
			return err
		}
		src, err := f.OpenRaw()
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			return err
		}
	}
	return zw.Close()
}

// registerDownloads serves packs whole (download/<id|all>.zip) and, for the
// launcher, per file: GET files/<id> lists a pack, POST files/<id> answers a
// zip of the requested entries.
func registerDownloads(g fiber.Router, d Deps, log *slog.Logger, rate fiber.Handler) {
	sl := newSlots(downloadSlots)
	g.Get("/download/:file", rate, sl.acquire, func(c fiber.Ctx) error {
		name := strings.TrimSuffix(c.Params("file"), ".zip")
		if name == c.Params("file") {
			return fiber.ErrNotFound
		}
		p, ready, err := publicPack(c.Context(), d, log, name)
		if err != nil {
			return err
		}
		if !ready {
			return notReady(c)
		}
		filename := fmt.Sprintf("%s-mods.zip", d.Cfg.ServerName)
		if name != "all" {
			filename = fmt.Sprintf("%s-%s.zip", d.Cfg.ServerName, name)
		}
		c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(filename)))
		c.Set("X-Checksum-SHA256", p.SHA256)
		return sendFile(c, p.Path, etag(p), "application/zip")
	})
	g.Get("/files/:id", rate, sl.acquire, func(c fiber.Ctx) error {
		if !wsidPath.MatchString(c.Params("id")) {
			return fiber.ErrNotFound
		}
		p, ready, err := publicPack(c.Context(), d, log, c.Params("id"))
		if err != nil {
			return err
		}
		if !ready {
			return notReady(c)
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		return sendFile(c, p.Files, etag(p), fiber.MIMEApplicationJSON)
	})
	g.Post("/files/:id", rate, sl.acquire, func(c fiber.Ctx) error {
		if !wsidPath.MatchString(c.Params("id")) {
			return fiber.ErrNotFound
		}
		var req publicapi.FilesRequest
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return fiber.NewError(http.StatusBadRequest, "the body must be a FilesRequest")
		}
		p, ready, err := publicPack(c.Context(), d, log, c.Params("id"))
		if err != nil {
			return err
		}
		if !ready {
			return notReady(c)
		}
		if req.SHA256 != p.SHA256 {
			return problem(c, http.StatusConflict, "the server's mods changed; sync again")
		}
		names, err := pickFiles(p.Files, req.Files)
		if err != nil {
			return err
		}
		zr, err := zip.OpenReader(p.Path)
		if err != nil {
			return err
		}
		pr, pw := io.Pipe()
		go func() {
			err := writeFiles(pw, &zr.Reader, names)
			zr.Close()
			if err != nil && !errors.Is(err, io.ErrClosedPipe) {
				log.Warn("public mod files", "err", err)
			}
			pw.CloseWithError(err)
		}()
		c.Set(fiber.HeaderContentType, "application/zip")
		// Closing the reader fails the writer's next write, which ends it.
		return streamBody(c, pr, -1, pr)
	})
}
