package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testHash = "$2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW"

func TestLoadRequiresAdminHash(t *testing.T) {
	t.Setenv("PANEL_ADMIN_PASSWORD_HASH", "")
	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "PANEL_ADMIN_PASSWORD_HASH")
}

func TestPlaintextHashRejected(t *testing.T) {
	t.Setenv("PANEL_ADMIN_PASSWORD_HASH", "hunter2")
	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a bcrypt hash")
}

func TestAllErrorsReported(t *testing.T) {
	t.Setenv("PANEL_ADMIN_PASSWORD_HASH", testHash)
	t.Setenv("PANEL_PORT", "notanumber")
	t.Setenv("PANEL_DRIVERS", "bogus")
	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "PANEL_PORT")
	require.Contains(t, err.Error(), "PANEL_DRIVERS")
}

func TestDriverScenarioPair(t *testing.T) {
	base := Config{AdminHash: testHash, Drivers: DriversReal, CookieSecure: CookieSecureAuto, LogLevel: "info",
		Port: 8080, ConsoleRing: 10, ServerName: "s", Timezone: "UTC", Scenario: "idle"}
	require.NoError(t, base.Validate())

	c := base
	c.Scenario = "crash-loop"
	require.ErrorContains(t, c.Validate(), "PANEL_SCENARIO")

	c = base
	c.Drivers = "bogus"
	require.ErrorContains(t, c.Validate(), "PANEL_DRIVERS")

	c = base
	c.Drivers = DriversFake
	c.Scenario = "crash-loop"
	require.NoError(t, c.Validate())
}

func TestPublicURL(t *testing.T) {
	base := Config{AdminHash: testHash, Drivers: DriversReal, CookieSecure: CookieSecureAuto, LogLevel: "info",
		Port: 8080, ConsoleRing: 10, ServerName: "s", Timezone: "UTC", Scenario: "idle"}
	for _, ok := range []string{"https://pz.example.com", "http://203.0.113.7:8080/", "https://[2001:db8::1]:8443"} {
		c := base
		c.PublicURL = ok
		require.NoError(t, c.Validate(), ok)
	}
	for _, bad := range []string{"pz.example.com", "pz.example.com:8080", "//pz.example.com", "ftp://pz.example.com",
		"https://", "https://pz.example.com/panel", "https://user:pw@pz.example.com", "https://pz.example.com?x=1",
		"https://pz.example.com#x", "https://pz.example.com:99999", "https://2001:db8::1"} {
		c := base
		c.PublicURL = bad
		require.ErrorContains(t, c.Validate(), "PANEL_PUBLIC_URL", bad)
	}

	require.Equal(t, "https://[2001:db8::1]:8443", Config{PublicURL: "https://[2001:db8::1]:8443/"}.PublicOrigin())
}

func TestPublicGameAddress(t *testing.T) {
	base := Config{AdminHash: testHash, Drivers: DriversReal, CookieSecure: CookieSecureAuto, LogLevel: "info",
		Port: 8080, ConsoleRing: 10, ServerName: "s", Timezone: "UTC", Scenario: "idle"}
	for _, ok := range []string{"play.example.com", "203.0.113.7:26261", "[2001:db8::1]", "[2001:db8::1]:16261"} {
		c := base
		c.PublicGameAddress = ok
		require.NoError(t, c.Validate(), ok)
	}
	for _, bad := range []string{"udp://play.example.com", "https://play.example.com", "play.example.com/x",
		"2001:db8::1", "play.example.com:0", "play.example.com:99999", "user@play.example.com", ":16261"} {
		c := base
		c.PublicGameAddress = bad
		require.ErrorContains(t, c.Validate(), "PANEL_PUBLIC_GAME_ADDRESS", bad)
	}

	addr := func(c Config) string { h, p := c.GameAddress(); return fmt.Sprintf("%s|%d", h, p) }
	require.Equal(t, "|0", addr(Config{}))
	require.Equal(t, "pz.example.com|0", addr(Config{PublicURL: "https://pz.example.com:8443"}),
		"falls back to the URL's host, never its (web) port")
	require.Equal(t, "2001:db8::1|16261", addr(Config{PublicURL: "https://pz.example.com", PublicGameAddress: "[2001:db8::1]:16261"}))
}

func TestRedactedHasNoSecrets(t *testing.T) {
	c := Config{AdminHash: testHash, JWTSecret: "jwt-secret-value", ModsToken: "mods-token-value", RCONPassword: "rconpw-value"}
	r := c.Redacted()
	for _, s := range []string{testHash, "jwt-secret-value", "mods-token-value", "rconpw-value"} {
		require.False(t, strings.Contains(r, s), "leaked %q", s)
	}
}
