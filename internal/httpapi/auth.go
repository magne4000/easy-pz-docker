package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/storage/memory/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/magne4000/easy-pz-docker/internal/app"
)

const (
	sessionCookie = "pzman_session"
	csrfCookie    = "pzman_csrf"
	csrfHeader    = "X-CSRF-Token"
	publicMeta    = "public"
	// pelicanSessionTTL bounds a Pelican session, which never slides: a
	// permission revoked in Pelican applies within it.
	pelicanSessionTTL = time.Hour
)

// auth: Docker mode signs the admin in with a password and keeps the session
// in a cookie (CSRF-protected). Pelican mode sets no cookie at all (cookies
// are shared by every port of the node's host, so another tenant's web server
// would receive them): the handoff returns a session token that the SPA sends
// as Authorization: Bearer, like the panel's single-use Pelican tokens.
type auth struct {
	cfg    app.Config
	log    *slog.Logger
	secret []byte
	hash   []byte
	now    func() time.Time
	// pelican verifies Pelican's tokens; nil in Docker mode.
	pelican *pelican
	tickets *tickets
}

func newAuth(cfg app.Config, log *slog.Logger) (*auth, error) {
	secret := []byte(cfg.JWTSecret)
	if len(secret) == 0 {
		// Accepted trade-off: a restart invalidates open sessions.
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
	}
	a := &auth{cfg: cfg, log: log, secret: secret, hash: []byte(cfg.AdminHash), now: time.Now}
	now := func() time.Time { return a.now() }
	a.tickets = newTickets(now)
	if cfg.Pelican() {
		var err error
		if a.pelican, err = newPelican(cfg.PelicanServer, cfg.PelicanPublicKey, now); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// secure implements PANEL_COOKIE_SECURE: auto trusts X-Forwarded-Proto
// only from allowlisted proxies, which is what c.Scheme() already enforces.
func (a *auth) secure(c fiber.Ctx) bool {
	switch a.cfg.CookieSecure {
	case app.CookieSecureTrue:
		return true
	case app.CookieSecureFalse:
		return false
	}
	return c.Scheme() == "https"
}

func problem(c fiber.Ctx, code int, detail string) error {
	return c.Status(code).JSON(huma.ErrorModel{Title: http.StatusText(code), Status: code, Detail: detail}, "application/problem+json")
}

func (a *auth) csrf() fiber.Handler {
	if a.pelican != nil {
		// No cookies, hence no ambient credential a cross-site request could
		// ride on: every request authenticates with a header (or a ticket).
		return func(c fiber.Ctx) error { return c.Next() }
	}
	// One storage shared by the Secure and non-Secure cookie variants so they
	// see the same tokens.
	storage := memory.New()
	mk := func(secure bool) fiber.Handler {
		return csrf.New(csrf.Config{
			Storage:        storage,
			CookieName:     csrfCookie,
			CookiePath:     "/",
			CookieSameSite: "Strict",
			CookieSecure:   secure,
			IdleTimeout:    a.cfg.SessionTTL,
			Extractor:      extractors.FromHeader(csrfHeader),
			ErrorHandler: func(c fiber.Ctx, err error) error {
				return problem(c, http.StatusForbidden, "CSRF check failed: "+err.Error())
			},
		})
	}
	secure, plain := mk(true), mk(false)
	return func(c fiber.Ctx) error {
		if a.secure(c) {
			return secure(c)
		}
		return plain(c)
	}
}

func (a *auth) loginLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Next:                   func(c fiber.Ctx) bool { return c.Path() != "/api/auth/login" },
		Max:                    5,
		Expiration:             time.Minute,
		SkipSuccessfulRequests: true,
		KeyGenerator:           func(c fiber.Ctx) string { return c.IP() },
		LimitReached: func(c fiber.Ctx) error {
			return problem(c, http.StatusTooManyRequests, "too many login attempts, try again in a minute")
		},
	})
}

// claims of pzman's own session token (the Docker cookie, the Pelican bearer).
type claims struct {
	jwt.RegisteredClaims
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scp,omitempty"`
}

func (a *auth) issue(p principal) (string, time.Time, error) {
	now := a.now()
	exp := now.Add(a.cfg.SessionTTL)
	if a.pelican != nil {
		exp = now.Add(pelicanSessionTTL)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject: p.Subject, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
	}, Name: p.Name, Scopes: p.Scopes})
	s, err := tok.SignedString(a.secret)
	return s, exp, err
}

// verify reads a session token: in Docker mode the admin's, with every
// scope; in Pelican mode a handoff's, with the scopes Pelican granted.
func (a *auth) verify(raw string) (principal, time.Time, error) {
	var cl claims
	_, err := jwt.ParseWithClaims(raw, &cl, func(*jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(a.now))
	if err != nil {
		return principal{}, time.Time{}, err
	}
	exp := cl.ExpiresAt.Time
	if a.pelican != nil {
		if cl.Subject == "" {
			return principal{}, time.Time{}, errors.New("no subject")
		}
		return principal{Subject: cl.Subject, Name: cl.Name, Scopes: knownScopes(cl.Scopes), Pelican: true, Expires: exp}, exp, nil
	}
	if subtle.ConstantTimeCompare([]byte(cl.Subject), []byte(a.cfg.AdminUser)) != 1 {
		return principal{}, time.Time{}, errors.New("unknown subject")
	}
	p := a.admin()
	p.Expires = exp
	return p, exp, nil
}

// admin is the password session's principal: every scope.
func (a *auth) admin() principal {
	return principal{Subject: a.cfg.AdminUser, Name: a.cfg.AdminUser, Scopes: allScopes}
}

func (a *auth) setCookie(c fiber.Ctx, value string, exp time.Time) {
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: value, Path: "/", HTTPOnly: true, SameSite: fiber.CookieSameSiteStrictMode,
		Secure: a.secure(c), Expires: exp})
}

// isSessionToken tells pzman's session tokens (HS256) from the plugin's
// single-use tokens (EdDSA) by their header, before either is verified.
func isSessionToken(tok string) bool {
	t, _, err := jwt.NewParser().ParseUnverified(tok, &jwt.RegisteredClaims{})
	return err == nil && t.Method == jwt.SigningMethodHS256
}

// authenticate finds the request's principal. Pelican mode: the bearer is a
// pzman session token (the SPA; no sliding) or a single-use Pelican token
// (service calls). Docker mode: the session cookie, which slides forward once
// it is half-way to expiry.
func (a *auth) authenticate(c fiber.Ctx) (principal, string, error) {
	if a.pelican != nil {
		tok := bearer(c)
		switch {
		case tok == "":
			return principal{}, "", errors.New("authentication required")
		case len(tok) > maxBearerBytes:
			return principal{}, "bearer", errors.New("token too long")
		case isSessionToken(tok):
			p, _, err := a.verify(tok)
			if err != nil {
				return principal{}, "session", fmt.Errorf("the session ended (%w): open easy-pz from Pelican again", err)
			}
			return p, "session", nil
		}
		p, err := a.pelican.verify(tok)
		return p, "bearer", err
	}
	p, exp, err := a.verify(c.Cookies(sessionCookie))
	if err != nil {
		return principal{}, "", errors.New("authentication required")
	}
	if exp.Sub(a.now()) < a.cfg.SessionTTL/2 {
		if tok, exp, err := a.issue(p); err == nil {
			a.setCookie(c, tok, exp)
		}
	}
	return p, "session", nil
}

// middleware rejects every non-public operation without a valid session,
// bearer token or ticket, or without the scope the operation needs; in
// Pelican mode it logs every write with the Pelican user (audit).
func (a *auth) middleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op != nil && op.Metadata[publicMeta] == true {
			next(ctx)
			return
		}
		c := humafiber.Unwrap(ctx)
		method, path := c.Method(), c.Path() // never the query: it may carry a ticket
		var p principal
		var via string
		var err error
		if id := c.Query("ticket"); id != "" && op != nil && op.Metadata[ticketMeta] == true {
			via = "ticket"
			p, err = a.tickets.use(id, path)
		} else {
			p, via, err = a.authenticate(c)
		}
		if err != nil {
			msg := err.Error()
			switch via {
			case "bearer":
				msg = "bearer token refused: " + msg
				a.log.Warn("pelican bearer token refused", "method", method, "path", path, "err", err)
			case "ticket":
				msg = "ticket refused: " + msg
			}
			huma.WriteErr(api, ctx, http.StatusUnauthorized, msg)
			return
		}
		need, ok := "", false
		if op != nil {
			need, ok = requiredScope(op)
		}
		if !ok || !p.has(need) {
			msg := "this operation is not allowed to anyone"
			if ok {
				msg = fmt.Sprintf("permission denied: this needs the %q scope", need)
				if p.Pelican {
					msg = fmt.Sprintf("permission denied: this needs the easy-pz.%s permission in Pelican", need)
				}
			}
			if p.Pelican {
				a.log.Warn("pelican action refused", "user", p.Name, "sub", p.Subject, "via", via, "method", method, "path", path,
					"need", need)
			}
			huma.WriteErr(api, ctx, http.StatusForbidden, msg)
			return
		}
		ctx = huma.WithValue(ctx, ctxKey{}, p)
		next(ctx)
		if p.Pelican && method != http.MethodGet {
			a.log.Info("pelican action", "user", p.Name, "sub", p.Subject, "via", via, "method", method, "path", path,
				"status", ctx.Status())
		}
	}
}

type SessionOutput struct {
	Body struct {
		Authenticated bool      `json:"authenticated"`
		Username      string    `json:"username,omitempty" doc:"PANEL_ADMIN_USER, or the Pelican username"`
		Scopes        []string  `json:"scopes" nullable:"false" enum:"read,control,console,mods,config,backups,schedules,settings" doc:"what the session may do (permissions contract); every scope for a password session"`
		ExpiresAt     time.Time `json:"expiresAt,omitzero"`
		Host          string    `json:"host" enum:"docker,pelican" doc:"where pzman runs (PZMAN_HOST); pelican: no password login, open easy-pz from Pelican"`
		// Set in Pelican mode, also when signed out: the login page links there.
		PelicanPanelURL string `json:"pelicanPanelUrl,omitempty" doc:"pelican: the panel's URL (PZMAN_PELICAN_PANEL_URL)"`
		PelicanOpenURL  string `json:"pelicanOpenUrl,omitempty" doc:"pelican: the panel's Open easy-pz route for this server; add ?to=<percent-encoded path> to sign in again and come back there"`
		Token           string `json:"token,omitempty" doc:"pelican, sign-in only: the session token, sent as Authorization: Bearer (no cookies on Pelican); it ends at expiresAt and never slides"`
	}
}

// session is the output for p (nil: signed out).
func (a *auth) session(p *principal, exp time.Time) *SessionOutput {
	out := &SessionOutput{}
	b := &out.Body
	b.Scopes, b.Host = []string{}, string(app.HostDocker)
	if a.cfg.Pelican() {
		b.Host, b.PelicanPanelURL = string(app.HostPelican), strings.TrimSuffix(a.cfg.PelicanPanelURL, "/")
		// The panel's routes use the server's uuid_short: the UUID's first 8 characters.
		b.PelicanOpenURL = b.PelicanPanelURL + "/easy-pz/" + a.cfg.PelicanServer[:min(8, len(a.cfg.PelicanServer))] + "/open"
	}
	if p != nil {
		b.Authenticated, b.Username, b.Scopes, b.ExpiresAt = true, p.Name, p.Scopes, exp
	}
	return out
}

// startSession issues p's session: a cookie in Docker mode, the token in the
// body in Pelican mode.
func (a *auth) startSession(ctx context.Context, p principal) (*SessionOutput, error) {
	tok, exp, err := a.issue(p)
	if err != nil {
		return nil, err
	}
	out := a.session(&p, exp)
	if a.pelican != nil {
		out.Body.Token = tok
	} else if c := fiberCtx(ctx); c != nil {
		a.setCookie(c, tok, exp)
	}
	return out, nil
}

type LoginInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"200"`
		Password string `json:"password" minLength:"1" maxLength:"200"`
	}
}

type PelicanLoginInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" maxLength:"8192" doc:"the handoff token from the #t= fragment of /auth/pelican"`
	}
}

type TicketInput struct {
	Body struct {
		Path string `json:"path" pattern:"^/api/(events|backups/[0-9]+/download)$" doc:"the request path the ticket is for"`
	}
}

type TicketOutput struct {
	Body struct {
		Ticket    string    `json:"ticket" doc:"pass as ?ticket= on that path, once"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
}

// RegisterAuthRoutes documents the auth operations for `pzman openapi`
// (handlers never run there).
func RegisterAuthRoutes(api huma.API) { (&auth{now: time.Now}).register(api) }

func (a *auth) register(api huma.API) {
	pub := map[string]any{publicMeta: true}
	huma.Register(api, huma.Operation{
		OperationID: "get-session", Method: http.MethodGet, Path: "/auth/session", Tags: []string{"auth"}, Metadata: pub,
		Summary: "Current session; also issues the CSRF cookie (Docker mode)",
	}, func(ctx context.Context, _ *struct{}) (*SessionOutput, error) {
		c := fiberCtx(ctx)
		if c == nil {
			return a.session(nil, time.Time{}), nil
		}
		raw := c.Cookies(sessionCookie)
		if a.pelican != nil {
			raw = ""
			if tok := bearer(c); isSessionToken(tok) { // never spend a single-use Pelican token here
				raw = tok
			}
		}
		if p, exp, err := a.verify(raw); err == nil {
			return a.session(&p, exp), nil
		}
		return a.session(nil, time.Time{}), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "login", Method: http.MethodPost, Path: "/auth/login", Tags: []string{"auth"}, Metadata: pub,
		Summary: "Password login (Docker mode only)",
		Errors:  []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *LoginInput) (*SessionOutput, error) {
		if a.cfg.Pelican() {
			return nil, huma.Error403Forbidden("password login is disabled on Pelican: open easy-pz from the server's page in Pelican (" +
				strings.TrimSuffix(a.cfg.PelicanPanelURL, "/") + ")")
		}
		userOK := subtle.ConstantTimeCompare([]byte(in.Body.Username), []byte(a.cfg.AdminUser)) == 1
		// Always run bcrypt so timing does not reveal whether the user exists.
		pwErr := bcrypt.CompareHashAndPassword(a.hash, []byte(in.Body.Password))
		if !userOK || pwErr != nil {
			a.log.Warn("failed login", "user", in.Body.Username)
			return nil, huma.Error401Unauthorized("invalid username or password")
		}
		return a.startSession(ctx, a.admin())
	})

	huma.Register(api, huma.Operation{
		OperationID: "login-pelican", Method: http.MethodPost, Path: "/auth/pelican", Tags: []string{"auth"}, Metadata: pub,
		Summary: "Trade a Pelican handoff token for a session (Pelican mode only)",
		Description: "The plugin's *Open easy-pz* redirects to `/auth/pelican#t=<token>`; the page posts the token here. Each token " +
			"works once. The answer's `token` is the session (one hour, no cookie): send it as `Authorization: Bearer`.",
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, in *PelicanLoginInput) (*SessionOutput, error) {
		if a.pelican == nil {
			return nil, huma.Error403Forbidden("Pelican sign-in is only available when pzman runs on Pelican")
		}
		p, err := a.pelican.verify(in.Body.Token)
		if err != nil {
			a.log.Warn("pelican sign-in refused", "err", err)
			return nil, huma.Error401Unauthorized("the Pelican sign-in link is invalid or was already used (" + err.Error() +
				"); open easy-pz from Pelican again")
		}
		if !p.has(scopeRead) {
			a.log.Warn("pelican sign-in refused: no read scope", "user", p.Name, "sub", p.Subject)
			return nil, huma.Error403Forbidden("your Pelican permissions do not include easy-pz.read")
		}
		a.log.Info("pelican sign-in", "user", p.Name, "sub", p.Subject, "scopes", strings.Join(p.Scopes, ","))
		return a.startSession(ctx, p)
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-ticket", Method: http.MethodPost, Path: "/auth/ticket", Tags: []string{"auth"},
		Metadata: map[string]any{scopeMeta: scopeRead},
		Summary:  "A single-use, 30 s ticket for the event stream or a backup download",
		Description: "For requests that cannot send an Authorization header (EventSource, download links): pass it as `?ticket=` " +
			"on the given path. It carries the session's user, scopes and expiry. At most 16 live tickets per user (429).",
		Errors: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *TicketInput) (*TicketOutput, error) {
		id, exp, err := a.tickets.issue(principalFrom(ctx), in.Body.Path)
		switch {
		case errors.Is(err, errTooManyTickets):
			return nil, huma.Error429TooManyRequests(err.Error())
		case err != nil:
			return nil, huma.Error503ServiceUnavailable(err.Error())
		}
		out := &TicketOutput{}
		out.Body.Ticket, out.Body.ExpiresAt = id, exp
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/auth/logout", Tags: []string{"auth"}, Metadata: pub,
		Summary:       "End the session (Pelican mode: the SPA forgets its token)",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if c := fiberCtx(ctx); c != nil && a.pelican == nil {
			a.setCookie(c, "", time.Unix(1, 0))
		}
		return nil, nil
	})
}
