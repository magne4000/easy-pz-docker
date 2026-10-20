package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// Scopes of the permissions contract: Pelican's easy-pz.<scope> subuser
// permissions grant them; a password session has them all.
const (
	scopeRead      = "read"
	scopeControl   = "control"
	scopeConsole   = "console"
	scopeMods      = "mods"
	scopeConfig    = "config"
	scopeBackups   = "backups"
	scopeSchedules = "schedules"
	scopeSettings  = "settings"
)

// allScopes is every scope, in the contract's order.
var allScopes = []string{scopeRead, scopeControl, scopeConsole, scopeMods, scopeConfig, scopeBackups, scopeSchedules, scopeSettings}

// writeScopes maps a Huma tag to the scope its writes need.
var writeScopes = map[string]string{
	"server":    scopeControl,
	"updates":   scopeControl,
	"console":   scopeConsole,
	"mods":      scopeMods,
	"config":    scopeConfig,
	"backups":   scopeBackups,
	"schedules": scopeSchedules,
	"system":    scopeSettings,
}

// scopeMeta, in an operation's Metadata, overrides the method/tag rule.
const scopeMeta = "scope"

// requiredScope is the scope an operation needs: its scopeMeta, else read for
// a GET (the event stream included), else its tag's write scope. ok is false
// for a write whose tag has no scope, which the middleware refuses.
func requiredScope(op *huma.Operation) (string, bool) {
	if s, ok := op.Metadata[scopeMeta].(string); ok {
		return s, true
	}
	if op.Method == http.MethodGet {
		return scopeRead, true
	}
	if len(op.Tags) != 1 {
		return "", false
	}
	s, ok := writeScopes[op.Tags[0]]
	return s, ok
}

// knownScopes keeps the scopes pzman knows, in the contract's order: a newer
// plugin may grant scopes this pzman does not have, which grant nothing.
func knownScopes(in []string) []string {
	out := []string{}
	for _, s := range allScopes {
		if slices.Contains(in, s) {
			out = append(out, s)
		}
	}
	return out
}

// principal is who makes a request: the admin of a password session, or the
// Pelican user of a handoff session or a bearer token.
type principal struct {
	Subject string // PANEL_ADMIN_USER, or the Pelican user's UUID
	Name    string // shown in the UI and the audit log
	Scopes  []string
	Pelican bool
}

func (p principal) has(scope string) bool { return slices.Contains(p.Scopes, scope) }

type ctxKey struct{}

// principalFrom is the authenticated caller of a Huma handler (zero, with no
// scope, outside one).
func principalFrom(ctx context.Context) principal {
	p, _ := ctx.Value(ctxKey{}).(principal)
	return p
}

// actionScopes: a schedule's action needs its own scope on top of
// schedules, so managing schedules cannot run what the caller may not.
var actionScopes = map[string]string{
	"restart": scopeControl, "stop": scopeControl, "start": scopeControl, "save": scopeControl,
	"broadcast": scopeControl, "backup": scopeBackups,
}

// requireAction refuses a schedule action whose scope the caller lacks.
func requireAction(ctx context.Context, action string) error {
	need, ok := actionScopes[action]
	if !ok || principalFrom(ctx).has(need) {
		return nil // unknown actions are refused by the scheduler's validation
	}
	return huma.Error403Forbidden(fmt.Sprintf("permission denied: a %s schedule also needs the %q scope (easy-pz.%s in Pelican)",
		action, need, need))
}
