package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

// The "Pelican tokens" contract: EdDSA (Ed25519) JWTs signed by the plugin,
// which keeps the private key; pzman.env only holds the public key
// (PZMAN_PELICAN_PUBLIC_KEY), so whoever reads the server's files cannot
// mint tokens. At most 60 s long, each usable once.
const (
	pelicanIssuer  = "pelican"
	tokenLeeway    = 30 * time.Second
	tokenMaxTTL    = 60 * time.Second
	maxLiveTokens  = 10000
	proxyHeader    = "X-EasyPZ-Proxy"
	proxyMaxSkew   = 60 * time.Second
	pelicanAudPfx  = "pzman:"
	bearerPrefix   = "Bearer "
	maxBearerBytes = 8192
)

// jtiFormat: ≥ 16 random bytes as base64url (22 characters) or hex (32).
var jtiFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{22,128}$`)

type pelicanClaims struct {
	jwt.RegisteredClaims
	Name   string   `json:"name"`
	Scopes []string `json:"scp"`
}

// pelican verifies the plugin's tokens and proxy signatures (Pelican mode).
type pelican struct {
	key    ed25519.PublicKey
	aud    string
	now    func() time.Time
	parser *jwt.Parser

	mu   sync.Mutex
	used map[string]time.Time // jti → when it may be forgotten
}

// newPelican takes the server's UUID and the plugin's public key (hex).
func newPelican(server, publicKey string, now func() time.Time) (*pelican, error) {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("PZMAN_PELICAN_PUBLIC_KEY is not an Ed25519 public key")
	}
	p := &pelican{key: key, aud: pelicanAudPfx + server, now: now, used: map[string]time.Time{}}
	p.parser = jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithIssuer(pelicanIssuer),
		jwt.WithAudience(p.aud), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(tokenLeeway),
		jwt.WithTimeFunc(now))
	return p, nil
}

var errTokenUsed = errors.New("token already used")

// verify checks a token and consumes its jti: a second use fails.
func (p *pelican) verify(raw string) (principal, error) {
	var cl pelicanClaims
	if _, err := p.parser.ParseWithClaims(raw, &cl, func(*jwt.Token) (any, error) { return p.key, nil }); err != nil {
		return principal{}, err
	}
	switch {
	case cl.IssuedAt == nil:
		return principal{}, errors.New("token has no iat")
	case cl.ExpiresAt.Sub(cl.IssuedAt.Time) > tokenMaxTTL:
		return principal{}, fmt.Errorf("token lives longer than %s", tokenMaxTTL)
	case !jtiFormat.MatchString(cl.ID):
		return principal{}, errors.New("token has no valid jti")
	case cl.Scopes == nil:
		return principal{}, errors.New("token has no scp")
	}
	if err := p.consume(cl.ID, cl.ExpiresAt.Add(tokenLeeway)); err != nil {
		return principal{}, err
	}
	name := cl.Name
	if name == "" {
		name = cl.Subject
	}
	return principal{Subject: cl.Subject, Name: name, Scopes: knownScopes(cl.Scopes), Pelican: true}, nil
}

// consume records jti until forget, the last moment the token could still
// pass verification. Past maxLiveTokens live entries it fails closed.
func (p *pelican) consume(jti string, forget time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.used[jti]; ok {
		return errTokenUsed
	}
	if len(p.used) >= maxLiveTokens {
		now := p.now()
		for k, t := range p.used {
			if now.After(t) {
				delete(p.used, k)
			}
		}
		if len(p.used) >= maxLiveTokens {
			return errors.New("too many tokens in flight, retry in a minute")
		}
	}
	p.used[jti] = forget
	return nil
}

// bearer is the request's bearer token, "" when it has none.
func bearer(c fiber.Ctx) string {
	h := c.Get(fiber.HeaderAuthorization)
	if len(h) > len(bearerPrefix) && strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(h[len(bearerPrefix):])
	}
	return ""
}

// proxyMessage is what the plugin signs for a proxied request: the
// X-EasyPZ-Proxy header is t=<unix>,sig=<base64url Ed25519 signature of it>.
func proxyMessage(t int64, method, path string) []byte {
	return fmt.Appendf(nil, "%d\n%s\n%s", t, strings.ToUpper(method), path)
}

// signed reports whether header is a valid, fresh signature of method and
// path (the raw request path, without the query).
func (p *pelican) signed(header, method, path string) bool {
	var ts, sig string
	for part := range strings.SplitSeq(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "sig":
			sig = v
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := p.now().Sub(time.Unix(t, 0)); d > proxyMaxSkew || d < -proxyMaxSkew {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return ed25519.Verify(p.key, proxyMessage(t, method, path), raw)
}

// clientIP is the address the public routes rate-limit and log. Docker mode:
// Fiber's (PANEL_TRUSTED_PROXIES). Pelican mode: every local client, the
// panel included, connects from the Wings gateway, so X-Forwarded-For counts
// only on requests the plugin signed; otherwise the connection's address.
func (p *pelican) clientIP(c fiber.Ctx) string {
	if p == nil {
		return c.IP()
	}
	if h := c.Get(proxyHeader); h != "" && p.signed(h, c.Method(), string(c.Request().URI().PathOriginal())) {
		first, _, _ := strings.Cut(c.Get(fiber.HeaderXForwardedFor), ",")
		if a, err := netip.ParseAddr(strings.TrimSpace(first)); err == nil {
			return a.Unmap().String()
		}
	}
	return c.RequestCtx().RemoteIP().String()
}
