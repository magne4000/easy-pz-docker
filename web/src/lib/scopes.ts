import type { Schemas } from "@/api/client";
import { useSession } from "@/api/queries";

// Scope is one permission of the session (the server's permissions contract):
// every GET needs read; each area's writes need their own scope.
export type Scope = Schemas["SessionOutputBody"]["scopes"][number];

// can reports whether a session holds a scope; signed-out or unknown sessions hold none.
export function can(
  session: Pick<Schemas["SessionOutputBody"], "authenticated" | "scopes"> | undefined,
  scope: Scope,
) {
  return !!session?.authenticated && (session.scopes ?? []).includes(scope);
}

// useCan reports whether the current session may use an area's write controls;
// the UI hides those it may not (the server refuses them anyway).
export function useCan(scope: Scope) {
  return can(useSession().data, scope);
}
