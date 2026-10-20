package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/static"

	"github.com/magne4000/easy-pz-docker/internal/app"
	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/console"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/sched"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/sys"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
	"github.com/magne4000/easy-pz-docker/internal/webui"
)

type Deps struct {
	Cfg      app.Config
	Version  string
	Bus      *events.Bus
	Ring     *console.Ring
	Store    *store.DB
	Coord    *sched.Coordinator
	Sched    *sched.Scheduler
	Mods     *mods.Service
	Packer   *mods.Packer
	Backups  *backup.Service
	Settings *settings.Store
	Tasks    *tasks.Registry
	Disk     *sys.Monitor
	// Ready backs /readyz. nil means "always ready".
	Ready func(ctx context.Context) error
}

type Server struct {
	cfg      app.Config
	log      *slog.Logger
	app      *fiber.App
	shutdown sync.Once

	// Run's listener: Shutdown closes it too, since a Fiber shutdown that
	// lands before Serve has registered it is a no-op (fatal boot errors
	// shut down right after start).
	lnMu   sync.Mutex
	ln     net.Listener
	closed bool
}

// NewAPI builds the Fiber app with the Huma group mounted on /api.
func NewAPI(version string, fcfg fiber.Config) (*fiber.App, fiber.Router, huma.API) {
	a := fiber.New(fcfg)
	hc := huma.DefaultConfig("pzman", version)
	hc.Info.Description = "Project Zomboid server manager API"
	hc.Servers = []*huma.Server{{URL: "/api"}}
	hc.DocsPath = ""
	g := a.Group("/api")
	return a, g, humafiber.NewWithGroup(a, g, hc)
}

func New(cfg app.Config, log *slog.Logger, deps Deps) (*Server, error) {
	proxyHeader := fiber.HeaderXForwardedFor
	if cfg.Pelican() {
		// Every local client, the panel included, connects from the Wings
		// gateway: X-Forwarded-For counts only when the plugin signed the
		// request (pelican.clientIP), never by address.
		proxyHeader = ""
	}
	a, g, api := NewAPI(deps.Version, fiber.Config{
		ReadTimeout: 15 * time.Second,
		// Bodies over the (default) BodyLimit reach the router as streams:
		// limitBody rejects them everywhere but the mod upload. Pre-parsing
		// would spool any multipart body to disk before routing.
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		ErrorHandler:                 problemErrorHandler(log),
		TrustProxy:                   true,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Proxies:  cfg.TrustedProxies,
			Loopback: true,
		},
		ProxyHeader: proxyHeader,
	})
	a.Use(limitBody(fiber.DefaultBodyLimit))
	s := &Server{cfg: cfg, log: log, app: a}
	auth, err := newAuth(cfg, log)
	if err != nil {
		return nil, err
	}

	a.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	a.Get(healthcheck.ReadinessEndpoint, healthcheck.New(healthcheck.Config{Probe: func(c fiber.Ctx) bool {
		if deps.Ready == nil {
			return true
		}
		if err := deps.Ready(c.Context()); err != nil {
			log.Warn("readiness probe failed", "err", err)
			return false
		}
		return true
	}}))
	a.Get("/robots.txt", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "public, max-age=86400")
		return c.SendString("User-agent: *\nDisallow: /\n")
	})

	g.Use(func(c fiber.Ctx) error {
		c.Locals(fiberCtxKey{}, c)
		c.Set("X-Accel-Buffering", "no")
		return c.Next()
	})
	g.Use(auth.csrf())
	g.Use(auth.loginLimiter())
	api.UseMiddleware(auth.middleware(api))
	auth.register(api)
	RegisterRoutes(api, deps)
	registerPublic(a, deps, log, auth.pelican)

	if !cfg.IsDev() && webui.Available() {
		mountStatic(a)
	}
	return s, nil
}

func (s *Server) App() *fiber.App { return s.app }

func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	// "tcp", not Fiber's tcp4 default: it also accepts ::1, which is what localhost resolves to on macOS.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.lnMu.Lock()
	if s.closed {
		s.lnMu.Unlock()
		return ln.Close()
	}
	s.ln = ln
	s.lnMu.Unlock()
	s.log.Info("http listening", "addr", addr)
	err = s.app.Listener(ln, fiber.ListenConfig{GracefulContext: ctx, DisableStartupMessage: true})
	if err != nil && (ctx.Err() != nil || errors.Is(err, net.ErrClosed)) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.lnMu.Lock()
	s.closed = true
	ln := s.ln
	s.lnMu.Unlock()
	var err error
	s.shutdown.Do(func() { err = s.app.ShutdownWithContext(ctx) })
	if ln != nil {
		_ = ln.Close() // already closed when Serve was running
	}
	return err
}

func problemErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		code := http.StatusInternalServerError
		detail := "internal server error"
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code, detail = fe.Code, fe.Message
		}
		if code >= 500 {
			log.Error("request failed", "method", c.Method(), "path", c.Path(), "err", err)
		}
		return c.Status(code).JSON(huma.ErrorModel{Title: http.StatusText(code), Status: code, Detail: detail}, "application/problem+json")
	}
}

type fiberCtxKey struct{}

// fiberCtx returns the Fiber context behind a Huma handler's context (nil in
// `pzman openapi`, where handlers never run).
func fiberCtx(ctx context.Context) fiber.Ctx {
	c, _ := ctx.Value(fiberCtxKey{}).(fiber.Ctx)
	return c
}

func noCache(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-cache")
	return nil
}

func serveHTML(name string, extra func(fiber.Ctx)) fiber.Handler {
	return func(c fiber.Ctx) error {
		b, err := fs.ReadFile(webui.FS(), name)
		if err != nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		if extra != nil {
			extra(c)
		}
		return c.Status(http.StatusOK).Send(b)
	}
}

func isAPI(c fiber.Ctx) bool {
	return strings.HasPrefix(c.Path(), "/api/") || c.Path() == "/api"
}

func mountStatic(a *fiber.App) {
	a.Get("/assets/*", static.New("assets", static.Config{
		FS: webui.FS(),
		ModifyResponse: func(c fiber.Ctx) error {
			c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
			return nil
		},
	}))
	a.Get("/*", static.New("", static.Config{
		FS:         webui.FS(),
		IndexNames: []string{"index.html"},
		// Without this the fallback swallows /api: NotFoundHandler ends the chain.
		Next:            isAPI,
		ModifyResponse:  noCache,
		NotFoundHandler: serveHTML("index.html", nil),
	}))
}
