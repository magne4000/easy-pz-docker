package app

import (
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

func TestRedactedHasNoSecrets(t *testing.T) {
	c := Config{AdminHash: testHash, JWTSecret: "jwt-secret-value", ModsToken: "mods-token-value", RCONPassword: "rconpw-value"}
	r := c.Redacted()
	for _, s := range []string{testHash, "jwt-secret-value", "mods-token-value", "rconpw-value"} {
		require.False(t, strings.Contains(r, s), "leaked %q", s)
	}
}
