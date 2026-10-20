// Pelican's "Open easy-pz" opens /auth/pelican#t=<token>[&to=<path>]: a
// single-use, short-lived sign-in token in the fragment, so it never reaches
// server logs, and where to go once signed in (a renewed session returns to
// the page it ended on).

import { safeReturnPath } from "@/api/session";

export type Handoff = { token: string; to: string | null };

// takeHandoff returns the fragment's token and return path and removes the
// fragment from the address bar and history at once, so the token is neither
// shown, bookmarked nor replayed. The token is "" when there is none; `to`
// is kept only when it is a path on this origin.
export function takeHandoff(loc: Location = window.location, hist: History = window.history): Handoff {
  if (!loc.hash) return { token: "", to: null };
  const params = new URLSearchParams(loc.hash.slice(1));
  hist.replaceState(hist.state, "", loc.pathname + loc.search);
  return { token: params.get("t")?.trim() ?? "", to: safeReturnPath(params.get("to"), loc.origin) };
}

let attempt: { to: string | null; result: Promise<unknown> } | null = null;

// handoffOnce posts the fragment's token exactly once: the token is
// single-use, and StrictMode runs effects twice. Later calls without a new
// token share the first attempt; a new token (the fragment changed in place)
// starts a new attempt. Returns null when no token was ever found.
export function handoffOnce<T>(
  post: (token: string) => Promise<T>,
): { to: string | null; result: Promise<T> } | null {
  const h = takeHandoff();
  if (h.token) attempt = { to: h.to, result: post(h.token) };
  return attempt as { to: string | null; result: Promise<T> } | null;
}
