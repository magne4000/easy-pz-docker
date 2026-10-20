import createClient from "openapi-fetch";
import type { components, paths } from "./schema";
import { sessionToken } from "./session";

export type Schemas = components["schemas"];

const CSRF_COOKIE = "pzman_csrf";
const CSRF_HEADER = "X-CSRF-Token";
const SAFE = new Set(["GET", "HEAD", "OPTIONS"]);

function csrfToken(): string | undefined {
  return document.cookie
    .split("; ")
    .find((c) => c.startsWith(`${CSRF_COOKIE}=`))
    ?.slice(CSRF_COOKIE.length + 1);
}

export class ApiError extends Error {
  status: number;
  problem?: Schemas["ErrorModel"];
  constructor(status: number, problem?: Schemas["ErrorModel"]) {
    const detail = problem?.errors?.length
      ? problem.errors.map((e) => `${e.location ? `${e.location}: ` : ""}${e.message}`).join("; ")
      : problem?.detail;
    super(detail || problem?.title || `HTTP ${status}`);
    this.status = status;
    this.problem = problem;
  }
}

type Listener = () => void;
const unauthorizedListeners = new Set<Listener>();
export function onUnauthorized(l: Listener) {
  unauthorizedListeners.add(l);
  return () => {
    unauthorizedListeners.delete(l);
  };
}

// Sign-in endpoints answer 401 for a bad password or link; that is no ended session.
const signIn = /\/api\/auth\/(login|pelican)$/;

// csrfFetch authenticates a request: with the Pelican session token as a
// bearer (no CSRF: no cookie is involved), or, in Docker mode, with the
// session cookie plus the double-submit token, fetching a fresh one and
// retrying once when the server lost it (e.g. after a restart).
async function csrfFetch(input: Request): Promise<Response> {
  const bearer = sessionToken();
  let res: Response;
  if (bearer) {
    input.headers.set("Authorization", `Bearer ${bearer}`);
    res = await fetch(input);
  } else {
    const unsafe = !SAFE.has(input.method);
    const retry = unsafe ? input.clone() : null;
    const withToken = (req: Request) => {
      const token = csrfToken();
      if (unsafe && token) req.headers.set(CSRF_HEADER, token);
      return req;
    };
    if (unsafe && !csrfToken()) await fetch("/api/auth/session", { credentials: "same-origin" });
    res = await fetch(withToken(input));
    if (res.status === 403 && retry) {
      const body = await res
        .clone()
        .json()
        .catch(() => null);
      if (typeof body?.detail === "string" && body.detail.startsWith("CSRF")) {
        await fetch("/api/auth/session", { credentials: "same-origin" });
        res = await fetch(withToken(retry));
      }
    }
  }
  if (res.status === 401 && !signIn.test(new URL(input.url).pathname)) {
    for (const l of unauthorizedListeners) l();
  }
  return res;
}

export const api = createClient<paths>({ baseUrl: "/api", credentials: "same-origin", fetch: csrfFetch });

// ticketed returns path with a fresh single-use ?ticket= when the session is
// a bearer token (EventSource and download links cannot send headers), else
// path itself: the cookie goes along.
export async function ticketed(path: "/api/events" | `/api/backups/${number}/download`): Promise<string> {
  if (!sessionToken()) return path;
  const { ticket } = await unwrap(api.POST("/auth/ticket", { body: { path } }));
  return `${path}?ticket=${encodeURIComponent(ticket)}`;
}

// unwrap turns an openapi-fetch result into data or a thrown ApiError.
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await p;
  if (!response.ok) throw new ApiError(response.status, error as Schemas["ErrorModel"] | undefined);
  return data as T;
}

type XhrResult = { status: number; response: unknown };

// send POSTs once with XMLHttpRequest, the browser API that reports upload
// progress (fetch does not).
function send(path: string, body: FormData, onProgress?: (loaded: number, total: number) => void) {
  const { promise, resolve, reject } = Promise.withResolvers<XhrResult>();
  const xhr = new XMLHttpRequest();
  xhr.open("POST", `/api${path}`);
  const bearer = sessionToken();
  const token = csrfToken();
  if (bearer) xhr.setRequestHeader("Authorization", `Bearer ${bearer}`);
  else if (token) xhr.setRequestHeader(CSRF_HEADER, token);
  xhr.responseType = "json";
  xhr.upload.onprogress = (e) => onProgress?.(e.loaded, e.total);
  xhr.onload = () => resolve({ status: xhr.status, response: xhr.response });
  xhr.onerror = () => reject(new Error("the upload was interrupted"));
  xhr.send(body);
  return promise;
}

// upload POSTs a multipart body; like csrfFetch, it fetches a fresh CSRF
// token and retries once when the server lost the old one.
export async function upload<T>(
  path: string,
  body: FormData,
  onProgress?: (loaded: number, total: number) => void,
): Promise<T> {
  if (!sessionToken() && !csrfToken()) await fetch("/api/auth/session", { credentials: "same-origin" });
  let res = await send(path, body, onProgress);
  if (res.status === 403 && (res.response as Schemas["ErrorModel"] | null)?.detail?.startsWith("CSRF")) {
    await fetch("/api/auth/session", { credentials: "same-origin" });
    res = await send(path, body, onProgress);
  }
  if (res.status >= 200 && res.status < 300) return res.response as T;
  if (res.status === 401) for (const l of unauthorizedListeners) l();
  throw new ApiError(res.status, (res.response ?? undefined) as Schemas["ErrorModel"] | undefined);
}
