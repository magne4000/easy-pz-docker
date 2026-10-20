// Pelican's "Open easy-pz" opens /auth/pelican#t=<token>: a single-use,
// short-lived sign-in token in the fragment, so it never reaches server logs.

// takeHandoffToken returns the #t= token and removes the fragment from the
// address bar and history at once, so it is neither shown, bookmarked nor
// replayed. Returns "" when there is none.
export function takeHandoffToken(loc: Location = window.location, hist: History = window.history): string {
  if (!loc.hash) return "";
  const token = new URLSearchParams(loc.hash.slice(1)).get("t")?.trim() ?? "";
  hist.replaceState(hist.state, "", loc.pathname + loc.search);
  return token;
}

let attempt: Promise<unknown> | null = null;

// handoffOnce posts the fragment's token exactly once: the token is
// single-use, and StrictMode runs effects twice. Later calls without a new
// token share the first attempt's promise; a new token (the fragment changed
// in place) starts a new attempt. Returns null when no token was ever found.
export function handoffOnce<T>(post: (token: string) => Promise<T>): Promise<T> | null {
  const token = takeHandoffToken();
  if (token) attempt = post(token);
  return attempt as Promise<T> | null;
}
