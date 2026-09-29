// Rejection patterns ported as data from zomboid-control-panel (fpsacha/zomboid-control-panel, MIT License, "Copyright (c) 2025 [Your Name or Username]"), server/services/rcon.js.
package rcon

import (
	"regexp"
	"strings"
)

type Rejection struct {
	Pattern  *regexp.Regexp
	Describe func(text string) string
}

type RejectedError struct {
	Response string
	Message  string
}

func (e *RejectedError) Error() string { return e.Message }

func same(msg string) func(string) string { return func(string) string { return msg } }
func suffix(s string) func(string) string { return func(t string) string { return t + s } }

var Rejections = []Rejection{
	{regexp.MustCompile(`(?i)^\s*Unknown command\b`), suffix(". This command is not available on this server build.")},
	{regexp.MustCompile(`(?i)^\s*Wrong arguments!?\s*$`), same("Wrong arguments. This command's syntax may have changed on this server build.")},
	{regexp.MustCompile(`(?i)^\s*Not enough rights\.?\s*$`), same("Not enough rights. The RCON account's role does not have permission to run this command.")},
	{regexp.MustCompile(`(?i)can be executed only from the game\.?\s*$`), suffix(". This command can only be run from in-game, not over RCON.")},
	{regexp.MustCompile(`(?i)^User .+ doesn't exist\.\s*$`), same("User doesn't exist. They may have disconnected, or the name may be misspelled.")},
	{regexp.MustCompile(`(?i)^\s*This user can't be kicked\.\s*$`), same("This user can't be kicked (protected account).")},
	{regexp.MustCompile(`(?i)^\s*No such user\s*$`), same("No such user. They must be currently connected for this command.")},
	{regexp.MustCompile(`(?i)^Invalid username ".*"\s*$`), suffix(". That username was not recognized.")},
	{regexp.MustCompile(`(?i)^Access Level '.+' unknown, list of access level:`), suffix(". That access level is not recognized on this server build.")},
	{regexp.MustCompile(`(?i)^You do not have sufficient rights to set this access level\.\s*$`), same("You do not have sufficient rights to set this access level.")},
	{regexp.MustCompile(`(?i)^User ".*" is not in the whitelist nor the server, use /adduser first\s*$`), suffix(".")},
	{regexp.MustCompile(`(?i)^\s*This user can't be banned\.\s*$`), same("This user can't be banned (protected account).")},
	{regexp.MustCompile(`(?i)^Cannot ban IP .+ \(Steam Relay shared address\)\. Use bansteamid or banuser instead\.\s*$`), suffix("")},
	{regexp.MustCompile(`(?i)^Cannot ban IP for player '.+' \(Steam Relay, real IP unavailable\)\. Use bansteamid or banuser without -ip\.\s*$`), suffix("")},
	{regexp.MustCompile(`(?i)^\s*A user with this name already exists\.?\s*$`), same("A user with this name already exists.")},
	{regexp.MustCompile(`(?i)^User ".*" is not in the whitelist, use /adduser first\s*$`), suffix(".")},
	{regexp.MustCompile(`(?i)^User .+ not found\s*$`), same("User not found.")},
	{regexp.MustCompile(`(?i)^\s*You don't have capability to ban/unban users\.\s*$`), same("You don't have capability to ban/unban users.")},
}

// Classify reports whether a raw response is one of PZ's known rejection strings.
func Classify(response string) *RejectedError {
	text := strings.TrimSpace(response)
	for _, r := range Rejections {
		if r.Pattern.MatchString(text) {
			return &RejectedError{Response: response, Message: r.Describe(text)}
		}
	}
	return nil
}

// ParsePlayers parses "Players connected (2):\n-alice\n-bob".
func ParsePlayers(resp string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(resp, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "-"); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}
