import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { keys } from "@/api/keys";
import { can, type Scope, useCan } from "./scopes";

type Session = Schemas["SessionOutputBody"];

const session = (s: Partial<Session>): Session => ({
  authenticated: true,
  host: "pelican",
  scopes: [],
  ...s,
});

describe("can", () => {
  it("grants exactly the session's scopes", () => {
    const s = session({ scopes: ["read", "console"] });
    expect(can(s, "read")).toBe(true);
    expect(can(s, "console")).toBe(true);
    expect(can(s, "control")).toBe(false);
    expect(can(s, "settings")).toBe(false);
  });

  it("grants nothing when signed out or unknown", () => {
    expect(can(undefined, "read")).toBe(false);
    expect(can(session({ authenticated: false, scopes: ["read"] }), "read")).toBe(false);
  });
});

// Tell React this test drives updates through act().
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// renderCan mounts useCan against a query cache seeded with the session.
function renderCan(s: Session, scope: Scope): boolean | undefined {
  const qc = new QueryClient();
  qc.setQueryData(keys.session, s);
  let result: boolean | undefined;
  function Probe() {
    result = useCan(scope);
    return null;
  }
  const root = createRoot(document.createElement("div"));
  act(() => root.render(createElement(QueryClientProvider, { client: qc }, createElement(Probe))));
  act(() => root.unmount());
  return result;
}

describe("useCan", () => {
  it("reads the scopes of the cached session", () => {
    const s = session({ scopes: ["read", "mods"] });
    expect(renderCan(s, "mods")).toBe(true);
    expect(renderCan(s, "backups")).toBe(false);
  });
});
