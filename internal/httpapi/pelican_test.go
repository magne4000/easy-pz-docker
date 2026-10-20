package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testServer = "9c3b6a3e-2f1d-4f6e-9a51-0d2b8c7e4a10"

// The plugin's keypair in these tests: sodium_crypto_sign_seed_keypair of
// the seed 000102…1f (testKey is its public key, PZMAN_PELICAN_PUBLIC_KEY).
var (
	testPriv = ed25519.NewKeyFromSeed([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23,
		24, 25, 26, 27, 28, 29, 30, 31})
	otherPriv = ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
)

const testKey = "03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8"

// Vectors made once with PHP's libsodium in docker php:8.4-cli, exactly as
// the contract specifies (and as the plugin signs):
//
//	$kp = sodium_crypto_sign_seed_keypair(hex2bin('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f'));
//	$b64 = fn ($s) => rtrim(strtr(base64_encode($s), '+/', '-_'), '=');
//	$f = JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE;
//	$in = $b64(json_encode(['alg' => 'EdDSA', 'typ' => 'JWT'], $f)) . '.' . $b64(json_encode(['iss' => 'pelican',
//	    'aud' => 'pzman:'.testServer, 'sub' => '5f0e1d2c-3b4a-4958-8776-655443322110', 'name' => 'zoë/admin',
//	    'scp' => ['read', 'control', 'future-scope'], 'iat' => 1700000000, 'exp' => 1700000060,
//	    'jti' => '00112233445566778899aabbccddeeff'], $f));
//	pluginToken: $in . '.' . $b64(sodium_crypto_sign_detached($in, sodium_crypto_sign_secretkey($kp)))
//	pluginProxy: 't=1700000000,sig=' . $b64(sodium_crypto_sign_detached("1700000000\nGET\n" . pluginPath, …))
//
// The plugin's own code (pelican-plugin/easy-pz/src/Support, mounted read-only
// in the same image) gives the same bytes for that secret key $sk:
// Jwt::mint($sk, testServer, '5f0e…2110', 'zoë/admin', [...], 1700000000,
// '00112233445566778899aabbccddeeff') and
// ProxySignature::header($sk, 'GET', pluginPath, 1700000000).
const (
	pluginToken = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJwZWxpY2FuIiwiYXVkIjoicHptYW46OWMzYjZhM2UtMmYxZC00ZjZlLTlhNTEtMGQyYjhjN2U0YTEwIiwic3ViIjoiNWYwZTFkMmMtM2I0YS00OTU4LTg3NzYtNjU1NDQzMzIyMTEwIiwibmFtZSI6Inpvw6svYWRtaW4iLCJzY3AiOlsicmVhZCIsImNvbnRyb2wiLCJmdXR1cmUtc2NvcGUiXSwiaWF0IjoxNzAwMDAwMDAwLCJleHAiOjE3MDAwMDAwNjAsImp0aSI6IjAwMTEyMjMzNDQ1NTY2Nzc4ODk5YWFiYmNjZGRlZWZmIn0.rX-loghGNHMA2KHusLxdyu3NoH_pKfcEWDaZxme0X-ma5rz-TPZV3rKXlMi7wMxp38DLvO1Y4wfI2-LX_g4-DA"
	pluginProxy = "t=1700000000,sig=QD874WAb-dpP9nbLK7fek_sYrbEPz_vcCY4t0ZAvJRwtdI1yh_vQ7i-fh7MmEVAfoq5AcFi4r1uSq2JG-hjsDA"
	pluginPath  = "/mods/tok/download/all%20mods.zip"
)

func at(unix int64) func() time.Time { return func() time.Time { return time.Unix(unix, 0) } }

func verifier(t *testing.T, key string, now func() time.Time) *pelican {
	t.Helper()
	p, err := newPelican(testServer, key, now)
	require.NoError(t, err)
	return p
}

// The plugin's own interop constants, copied byte for byte from
// pelican-plugin/easy-pz/tests/Unit/JwtTest.php (pelican/phase-3): seed = 32
// bytes of 0x01, token minted at 1700000000, proxy header for
// GET /mods/tok/download/x.zip at t=1700000000.
func TestPluginInteropConstants(t *testing.T) {
	const (
		server = "0d4c1a9e-0000-4000-8000-000000000001"
		key    = "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
		tok    = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9" +
			".eyJpc3MiOiJwZWxpY2FuIiwiYXVkIjoicHptYW46MGQ0YzFhOWUtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAxIiwic3ViIjoiYjNhMWMyZDQtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAyIiwibmFtZSI6ImFsaWNlIiwic2NwIjpbInJlYWQiLCJjb250cm9sIl0sImlhdCI6MTcwMDAwMDAwMCwiZXhwIjoxNzAwMDAwMDYwLCJqdGkiOiIwMDAxMDIwMzA0MDUwNjA3MDgwOTBhMGIwYzBkMGUwZiJ9" +
			".u0EV4ffjK_YUiLPen4Hw79FLj0cC-a5hgwdgGLpqraXN4L96jc_yoraIhveRVWNTiKyHI33TmpEsIYtTzaz1AQ"
		proxy = "t=1700000000,sig=0kTh6wkNGn9f8XRZKKjRaG8ruxRCXJTpIn7lezpyWaSUuOc8rL4YvBzT00Kq2D87VAh70jeaPV78n4K7TVSJCQ"
	)
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 1
	}
	priv := ed25519.NewKeyFromSeed(seed)
	require.Equal(t, key, hex.EncodeToString(priv.Public().(ed25519.PublicKey)))

	p, err := newPelican(server, key, at(1700000000))
	require.NoError(t, err)
	got, err := p.verify(tok)
	require.NoError(t, err)
	require.Equal(t, principal{Subject: "b3a1c2d4-0000-4000-8000-000000000002", Name: "alice",
		Scopes: []string{scopeRead, scopeControl}, Pelican: true}, got)
	_, err = p.verify(tok)
	require.ErrorIs(t, err, errTokenUsed)
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{}).SigningString()
	require.NoError(t, err)
	require.Equal(t, strings.SplitN(tok, ".", 2)[0], strings.SplitN(s, ".", 2)[0], "header {\"alg\":\"EdDSA\",\"typ\":\"JWT\"}")

	require.True(t, p.signed(proxy, http.MethodGet, "/mods/tok/download/x.zip"))
	require.Equal(t, proxy, signProxy(priv, 1700000000, http.MethodGet, "/mods/tok/download/x.zip"))
}

func TestPluginTokenVector(t *testing.T) {
	require.Equal(t, testKey, hex.EncodeToString(testPriv.Public().(ed25519.PublicKey)), "sodium's seed keypair is Go's")
	p := verifier(t, testKey, at(1700000010))
	got, err := p.verify(pluginToken)
	require.NoError(t, err)
	require.Equal(t, principal{Subject: "5f0e1d2c-3b4a-4958-8776-655443322110", Name: "zoë/admin",
		Scopes: []string{scopeRead, scopeControl}, Pelican: true}, got, "unknown scopes are ignored")
	_, err = p.verify(pluginToken)
	require.ErrorIs(t, err, errTokenUsed)

	_, err = verifier(t, testKey, at(1700000091)).verify(pluginToken)
	require.ErrorIs(t, err, jwt.ErrTokenExpired, "exp + 30 s leeway")
	_, err = verifier(t, testKey, at(1700000089)).verify(pluginToken)
	require.NoError(t, err, "within the leeway")
	other, err := newPelican("00000000-0000-0000-0000-000000000000", testKey, at(1700000010))
	require.NoError(t, err)
	_, err = other.verify(pluginToken)
	require.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)
	_, err = verifier(t, hex.EncodeToString(otherPriv.Public().(ed25519.PublicKey)), at(1700000010)).verify(pluginToken)
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)

	_, err = newPelican(testServer, testKey[2:], time.Now)
	require.ErrorContains(t, err, "not an Ed25519 public key")
}

// mint signs claims like the plugin, with a fresh jti unless given.
func mint(t *testing.T, key ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["jti"]; !ok {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		claims["jti"] = hex.EncodeToString(b)
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(key)
	require.NoError(t, err)
	return s
}

func tokenClaims(now time.Time, scopes ...string) jwt.MapClaims {
	return jwt.MapClaims{"iss": "pelican", "aud": "pzman:" + testServer, "sub": "user-uuid", "name": "alice",
		"scp": scopes, "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}
}

func TestTokenRules(t *testing.T) {
	now := time.Unix(1700000000, 0)
	p := verifier(t, testKey, func() time.Time { return now })
	with := func(edit func(jwt.MapClaims)) string {
		c := tokenClaims(now, scopeRead)
		edit(c)
		return mint(t, testPriv, c)
	}
	_, err := p.verify(with(func(jwt.MapClaims) {}))
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		tok  string
		want string
	}{
		"forged":       {mint(t, otherPriv, tokenClaims(now, scopeRead)), "signature is invalid"},
		"wrong issuer": {with(func(c jwt.MapClaims) { c["iss"] = "someone" }), "invalid issuer"},
		"wrong server": {with(func(c jwt.MapClaims) { c["aud"] = "pzman:other" }), "invalid audience"},
		"no audience":  {with(func(c jwt.MapClaims) { delete(c, "aud") }), "aud claim is required"},
		"expired": {with(func(c jwt.MapClaims) {
			c["iat"], c["exp"] = now.Add(-2*time.Minute).Unix(), now.Add(-31*time.Second).Unix()
		}), "expired"},
		"no exp": {with(func(c jwt.MapClaims) { delete(c, "exp") }), "exp claim is required"},
		"no iat": {with(func(c jwt.MapClaims) { delete(c, "iat") }), "no iat"},
		"iat in the future": {with(func(c jwt.MapClaims) {
			c["iat"], c["exp"] = now.Add(31*time.Second).Unix(), now.Add(61*time.Second).Unix()
		}), "used before issued"},
		"lives too long":   {with(func(c jwt.MapClaims) { c["exp"] = now.Add(61 * time.Second).Unix() }), "longer than 1m0s"},
		"no jti":           {with(func(c jwt.MapClaims) { c["jti"] = "" }), "no valid jti"},
		"short jti":        {with(func(c jwt.MapClaims) { c["jti"] = "abc" }), "no valid jti"},
		"no scp":           {with(func(c jwt.MapClaims) { delete(c, "scp") }), "no scp"},
		"scp not an array": {with(func(c jwt.MapClaims) { c["scp"] = "read" }), "cannot unmarshal"},
	} {
		_, err := p.verify(tc.tok)
		require.ErrorContains(t, err, tc.want, name)
	}

	// Algorithms other than EdDSA are refused: HMAC keyed with the public
	// key (algorithm confusion), and none.
	for _, m := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodHS512, jwt.SigningMethodNone} {
		c := tokenClaims(now, scopeRead)
		c["jti"] = "0123456789abcdef0123456789"
		key := any([]byte(testKey))
		if m == jwt.SigningMethodNone {
			key = jwt.UnsafeAllowNoneSignatureType
		}
		s, err := jwt.NewWithClaims(m, c).SignedString(key)
		require.NoError(t, err)
		_, err = p.verify(s)
		require.ErrorContains(t, err, "signing method", m.Alg())
	}

	// Within the leeway, and a token whose scopes are all unknown.
	tok := with(func(c jwt.MapClaims) {
		c["iat"], c["exp"] = now.Add(-80*time.Second).Unix(), now.Add(-20*time.Second).Unix()
		c["scp"] = []string{"teleport"}
	})
	got, err := p.verify(tok)
	require.NoError(t, err)
	require.Equal(t, []string{}, got.Scopes)
}

func TestJTIStoreIsBounded(t *testing.T) {
	now := time.Unix(1700000000, 0)
	p := verifier(t, testKey, func() time.Time { return now })
	for i := range maxLiveTokens {
		require.NoError(t, p.consume(fmt.Sprint(i), now.Add(90*time.Second)))
	}
	require.ErrorContains(t, p.consume("one-more", now.Add(90*time.Second)), "too many tokens")
	now = now.Add(91 * time.Second)
	require.NoError(t, p.consume("one-more", now.Add(90*time.Second)), "expired entries are dropped")
	require.Len(t, p.used, 1)
	require.ErrorIs(t, p.consume("one-more", now), errTokenUsed)
}

// signProxy is the plugin's X-EasyPZ-Proxy header.
func signProxy(key ed25519.PrivateKey, t int64, method, path string) string {
	return fmt.Sprintf("t=%d,sig=%s", t, base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, proxyMessage(t, method, path))))
}

func TestProxySignature(t *testing.T) {
	p := verifier(t, testKey, at(1700000000))
	require.Equal(t, pluginProxy, signProxy(testPriv, 1700000000, http.MethodGet, pluginPath))
	require.True(t, p.signed(pluginProxy, http.MethodGet, pluginPath), "the plugin's vector")
	require.False(t, p.signed(pluginProxy, http.MethodGet, "/mods/tok/download/all mods.zip"), "the raw path, not the decoded one")
	require.False(t, p.signed(pluginProxy, http.MethodHead, pluginPath), "another method")
	require.False(t, p.signed(pluginProxy, http.MethodGet, "/mods/tok/download/1.zip"), "another path")
	sig := strings.TrimPrefix(pluginProxy, "t=1700000000,sig=")
	require.False(t, p.signed("t=1700000000,sig=A"+sig[1:], http.MethodGet, pluginPath), "bad signature")
	require.False(t, p.signed("t=1700000001,sig="+sig, http.MethodGet, pluginPath), "t is signed")
	require.False(t, p.signed(pluginProxy+"==", http.MethodGet, pluginPath), "padding is not base64url without padding")
	require.False(t, p.signed("t=1700000000,sig=", http.MethodGet, pluginPath))
	require.False(t, p.signed("sig="+sig, http.MethodGet, pluginPath), "no t")
	require.False(t, p.signed(signProxy(otherPriv, 1700000000, http.MethodGet, pluginPath), http.MethodGet, pluginPath), "another key")

	require.True(t, verifier(t, testKey, at(1700000060)).signed(pluginProxy, http.MethodGet, pluginPath), "60 s old")
	require.False(t, verifier(t, testKey, at(1700000061)).signed(pluginProxy, http.MethodGet, pluginPath), "stale")
	require.False(t, verifier(t, testKey, at(1699999939)).signed(pluginProxy, http.MethodGet, pluginPath), "from the future")
}

func TestClientIP(t *testing.T) {
	now := time.Now()
	p := verifier(t, testKey, func() time.Time { return now })
	app := fiber.New(fiber.Config{TrustProxy: true, TrustProxyConfig: fiber.TrustProxyConfig{Loopback: true}})
	app.All("/*", func(c fiber.Ctx) error { return c.SendString(p.clientIP(c)) })
	ip := func(method, path string, hdr ...string) string {
		req, _ := http.NewRequest(method, "http://pzman"+path, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		b := make([]byte, 64)
		n, _ := resp.Body.Read(b)
		return string(b[:n])
	}
	sig := func(method, path string) string { return signProxy(testPriv, now.Unix(), method, path) }
	const path = "/mods/tok/files/12%2Fa%20b"
	require.Equal(t, "0.0.0.0", ip("GET", path, "X-Forwarded-For", "203.0.113.7"), "unsigned: the connection's address")
	require.Equal(t, "203.0.113.7", ip("GET", path+"?x=1", "X-Forwarded-For", "203.0.113.7", proxyHeader, sig("GET", path)),
		"signed over the raw path, without the query")
	require.Equal(t, "203.0.113.7", ip("POST", path, "X-Forwarded-For", "203.0.113.7", proxyHeader, sig("POST", path)),
		"the launcher's POST files/<id>")
	require.Equal(t, "2001:db8::1", ip("GET", path, "X-Forwarded-For", " 2001:db8::1 , 10.0.0.1", proxyHeader, sig("GET", path)))
	require.Equal(t, "0.0.0.0", ip("GET", path, "X-Forwarded-For", "203.0.113.7", proxyHeader, sig("POST", path)))
	require.Equal(t, "0.0.0.0", ip("GET", path, "X-Forwarded-For", "not-an-ip", proxyHeader, sig("GET", path)))
	require.Equal(t, "0.0.0.0", ip("GET", "/mods/tok/files/12/a%20b", "X-Forwarded-For", "203.0.113.7", proxyHeader, sig("GET", path)))

	var docker *pelican
	app = fiber.New(fiber.Config{TrustProxy: true, TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{"0.0.0.0"}},
		ProxyHeader: fiber.HeaderXForwardedFor})
	app.Get("/*", func(c fiber.Ctx) error { return c.SendString(docker.clientIP(c)) })
	require.Equal(t, "203.0.113.7", ip("GET", path, "X-Forwarded-For", "203.0.113.7"), "Docker mode: Fiber's trusted proxies")
}
