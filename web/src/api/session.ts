// Pelican mode sets no cookie (cookies are shared by every port of the node's
// host): the session token from the sign-in lives in localStorage, which is
// per origin, so per port, and goes out as Authorization: Bearer. Docker mode
// keeps its cookie session and never stores a token.

const TOKEN_KEY = "pzman.session";
const RENEW_KEY = "pzman.renewedAt";
// A renewal that comes back unauthenticated within this window is not retried:
// no redirect loop between pzman and the panel.
const RENEW_GUARD_MS = 30_000;

export function sessionToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setSessionToken(token: string | null) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    // storage unavailable: the session lasts as long as the page
  }
}

// safeReturnPath keeps a `to` only when it is a path on this origin, so the
// sign-in cannot be turned into a redirect elsewhere.
export function safeReturnPath(
  to: string | null | undefined,
  origin = window.location.origin,
): string | null {
  if (!to?.startsWith("/") || to.startsWith("//") || to.includes("\\") || [...to].some((c) => c < " "))
    return null;
  try {
    const u = new URL(to, origin);
    return u.origin === origin ? u.pathname + u.search + u.hash : null;
  } catch {
    return null;
  }
}

// renewURL is where an ended Pelican session signs in again: the panel's
// Open easy-pz route re-checks the permissions and comes back to `to`.
export function renewURL(openUrl: string, to: string): string {
  return `${openUrl}?to=${encodeURIComponent(to)}`;
}

// startRenewal drops the ended session and sends the browser to the panel,
// unless a renewal just happened (then it returns false: show the sign-in page).
export function startRenewal(openUrl: string, loc: Location = window.location): boolean {
  setSessionToken(null);
  try {
    const last = Number(sessionStorage.getItem(RENEW_KEY) ?? 0);
    if (Date.now() - last < RENEW_GUARD_MS) return false;
    sessionStorage.setItem(RENEW_KEY, String(Date.now()));
  } catch {
    return false;
  }
  loc.assign(renewURL(openUrl, loc.pathname + loc.search));
  return true;
}
