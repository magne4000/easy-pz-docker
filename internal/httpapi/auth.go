package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
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
)

type auth struct {
	cfg    app.Config
	log    *slog.Logger
	secret []byte
	hash   []byte
	now    func() time.Time
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
	return &auth{cfg: cfg, log: log, secret: secret, hash: []byte(cfg.AdminHash), now: time.Now}, nil
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

type claims struct {
	jwt.RegisteredClaims
}

func (a *auth) issue(user string) (string, time.Time, error) {
	now := a.now()
	exp := now.Add(a.cfg.SessionTTL)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{jwt.RegisteredClaims{
		Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
	}})
	s, err := tok.SignedString(a.secret)
	return s, exp, err
}

func (a *auth) verify(raw string) (*claims, error) {
	var cl claims
	_, err := jwt.ParseWithClaims(raw, &cl, func(*jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(a.now))
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(cl.Subject), []byte(a.cfg.AdminUser)) != 1 {
		return nil, errors.New("unknown subject")
	}
	return &cl, nil
}

func (a *auth) setCookie(c fiber.Ctx, value string, exp time.Time) {
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: value, Path: "/", HTTPOnly: true, SameSite: fiber.CookieSameSiteStrictMode,
		Secure: a.secure(c), Expires: exp})
}

type ctxKey struct{}

// middleware rejects every non-public operation without a valid session and
// slides the session forward once it is half-way to expiry.
func (a *auth) middleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if op := ctx.Operation(); op != nil && op.Metadata[publicMeta] == true {
			next(ctx)
			return
		}
		c := humafiber.Unwrap(ctx)
		cl, err := a.verify(c.Cookies(sessionCookie))
		if err != nil {
			huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		if cl.ExpiresAt != nil && cl.ExpiresAt.Sub(a.now()) < a.cfg.SessionTTL/2 {
			if tok, exp, err := a.issue(cl.Subject); err == nil {
				a.setCookie(c, tok, exp)
			}
		}
		next(huma.WithValue(ctx, ctxKey{}, cl.Subject))
	}
}

type SessionOutput struct {
	Body struct {
		Authenticated bool      `json:"authenticated"`
		User          string    `json:"user,omitempty"`
		ExpiresAt     time.Time `json:"expiresAt,omitzero"`
	}
}

type LoginInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"200"`
		Password string `json:"password" minLength:"1" maxLength:"200"`
	}
}

// RegisterAuthRoutes documents the auth operations for `pzman openapi`
// (handlers never run there).
func RegisterAuthRoutes(api huma.API) { (&auth{now: time.Now}).register(api) }

func (a *auth) register(api huma.API) {
	pub := map[string]any{publicMeta: true}
	huma.Register(api, huma.Operation{
		OperationID: "get-session", Method: http.MethodGet, Path: "/auth/session", Tags: []string{"auth"}, Metadata: pub,
		Summary: "Current session; also issues the CSRF cookie",
	}, func(ctx context.Context, _ *struct{}) (*SessionOutput, error) {
		out := &SessionOutput{}
		c := fiberCtx(ctx)
		if c == nil {
			return out, nil
		}
		if cl, err := a.verify(c.Cookies(sessionCookie)); err == nil {
			out.Body.Authenticated, out.Body.User = true, cl.Subject
			if cl.ExpiresAt != nil {
				out.Body.ExpiresAt = cl.ExpiresAt.Time
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "login", Method: http.MethodPost, Path: "/auth/login", Tags: []string{"auth"}, Metadata: pub,
		Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *LoginInput) (*SessionOutput, error) {
		userOK := subtle.ConstantTimeCompare([]byte(in.Body.Username), []byte(a.cfg.AdminUser)) == 1
		// Always run bcrypt so timing does not reveal whether the user exists.
		pwErr := bcrypt.CompareHashAndPassword(a.hash, []byte(in.Body.Password))
		if !userOK || pwErr != nil {
			a.log.Warn("failed login", "user", in.Body.Username)
			return nil, huma.Error401Unauthorized("invalid username or password")
		}
		tok, exp, err := a.issue(a.cfg.AdminUser)
		if err != nil {
			return nil, err
		}
		if c := fiberCtx(ctx); c != nil {
			a.setCookie(c, tok, exp)
		}
		out := &SessionOutput{}
		out.Body.Authenticated, out.Body.User, out.Body.ExpiresAt = true, a.cfg.AdminUser, exp
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/auth/logout", Tags: []string{"auth"}, Metadata: pub,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if c := fiberCtx(ctx); c != nil {
			a.setCookie(c, "", time.Unix(1, 0))
		}
		return nil, nil
	})
}
