package httpapi

import (
	"bufio"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// sessionFor signs a pzman session token like the handoff does (the test
// server's secret), ending at exp.
func sessionFor(t *testing.T, exp time.Time, scopes ...string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-uuid", "name": "alice", "scp": scopes,
		"iat": time.Now().Unix(), "exp": exp.Unix()}).SignedString([]byte(testConfig().JWTSecret))
	require.NoError(t, err)
	return s
}

// openStream connects to /api/events and returns a channel closed when the
// server ends the stream.
func openStream(t *testing.T, base, query, bearer string) <-chan struct{} {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/api/events"+query, nil)
	require.NoError(t, err)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
		}
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return done
}

// An open event stream ends with the session that opened it, so a session
// that ends (or a revoked permission) cannot keep receiving events.
func TestEventStreamEndsWithTheSession(t *testing.T) {
	s, _ := newTestServer(t, pelicanConfig())
	base := serve(t, s.App())
	waitEnd := func(done <-chan struct{}, within time.Duration, what string) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(within):
			t.Fatalf("%s: the stream outlived its credential", what)
		}
	}

	// Session bearer (jwt exp has 1 s granularity).
	exp := time.Now().Add(2 * time.Second)
	waitEnd(openStream(t, base, "", sessionFor(t, exp, scopeRead)), 5*time.Second, "session")

	// A ticket carries the expiry of the session that minted it.
	session := sessionFor(t, time.Now().Add(2*time.Second), scopeRead)
	resp := (&pelicanClient{t: t, app: s.App(), session: session}).do(http.MethodPost, "/api/auth/ticket", `{"path":"/api/events"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	tk := decode[struct {
		Ticket string `json:"ticket"`
	}](t, resp).Ticket
	waitEnd(openStream(t, base, "?ticket="+tk, ""), 5*time.Second, "ticket")

	// A single-use Pelican service token ends at its own exp (here: issued
	// 58 s ago, so 2 s left).
	c := tokenClaims(time.Now().Add(-58*time.Second), scopeRead)
	waitEnd(openStream(t, base, "", mint(t, testPriv, c)), 5*time.Second, "pelican token")

	// Until then, the stream stays open (heartbeats keep it alive).
	done := openStream(t, base, "", sessionFor(t, time.Now().Add(4*time.Second), scopeRead))
	select {
	case <-done:
		t.Fatal("a live session's stream ended early")
	case <-time.After(2 * time.Second):
	}
	waitEnd(done, 5*time.Second, "session, later")
}

func TestTicketOfAnEndedSession(t *testing.T) {
	_, a := scopeAPI(t, slog.New(slog.DiscardHandler))
	start := time.Now()
	a.now = func() time.Time { return start }
	id, _, err := a.tickets.issue(principal{Subject: "u", Scopes: []string{scopeRead}, Expires: start.Add(10 * time.Second)}, "/api/events")
	require.NoError(t, err)
	a.now = func() time.Time { return start.Add(11 * time.Second) }
	_, err = a.tickets.use(id, "/api/events")
	require.ErrorContains(t, err, "the session that made the ticket ended")
}

// One session cannot fill the shared ticket store.
func TestTicketsPerSubject(t *testing.T) {
	c := &pelicanClient{t: t, app: newServer(t, pelicanConfig())}
	alice := &pelicanClient{t: t, app: c.app, session: sessionFor(t, time.Now().Add(time.Hour), scopeRead)}
	ticket := func(cl *pelicanClient) int {
		return cl.do(http.MethodPost, "/api/auth/ticket", `{"path":"/api/events"}`).StatusCode
	}
	for i := range maxTicketsPerSubject {
		require.Equal(t, http.StatusOK, ticket(alice), i)
	}
	require.Equal(t, http.StatusTooManyRequests, ticket(alice))

	other, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "other-uuid", "scp": []string{scopeRead},
		"exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(testConfig().JWTSecret))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, ticket(&pelicanClient{t: t, app: c.app, session: other}), "other users are not affected")

	// Using a ticket frees its slot; so does expiry.
	tk := newTickets(time.Now)
	p := principal{Subject: "s"}
	var ids []string
	for range maxTicketsPerSubject {
		id, _, err := tk.issue(p, "/api/events")
		require.NoError(t, err)
		ids = append(ids, id)
	}
	_, _, err = tk.issue(p, "/api/events")
	require.ErrorIs(t, err, errTooManyTickets)
	_, err = tk.use(ids[0], "/api/events")
	require.NoError(t, err)
	_, _, err = tk.issue(p, "/api/events")
	require.NoError(t, err)
	later := time.Now().Add(ticketTTL + time.Second)
	tk.now = func() time.Time { return later }
	_, _, err = tk.issue(p, "/api/events")
	require.NoError(t, err, "expired tickets are swept")
	require.Equal(t, 1, tk.bySubject["s"])
}
