import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { keys } from "@/api/keys";
import { connectEvents, dispatch, eventActions } from "@/events";

// Pins the topic → query-key table against the server's event union, read
// from the committed OpenAPI document, so the two cannot drift silently.
function serverEventNames(): string[] {
  const doc = JSON.parse(readFileSync(resolve(import.meta.dirname, "../../api/openapi.json"), "utf8"));
  const schema = doc.paths["/events"].get.responses["200"].content["text/event-stream"].schema;
  return schema.items.oneOf
    .map((v: { properties: { event: { const: string } } }) => v.properties.event.const)
    .sort();
}

describe("events dispatch table", () => {
  it("maps exactly the server's event union", () => {
    expect(Object.keys(eventActions).sort()).toEqual(serverEventNames());
  });

  it("maps each topic to the right queries", () => {
    expect(eventActions["server:status"]).toEqual({ invalidate: [keys.server, keys.mods] });
    expect(eventActions["backups:changed"]).toEqual({ invalidate: [keys.backups] });
    expect(eventActions["mods:changed"]).toEqual({ invalidate: [keys.mods] });
    expect(eventActions["update:window"]).toEqual({ invalidate: [keys.updates, keys.server] });
    expect(eventActions["console:line"]).toBe("console");
    expect(eventActions["stream:desync"]).toBe("all");
  });

  it("stream:desync refetches everything", () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    dispatch(qc, "stream:desync", { dropped: 3 });
    expect(spy).toHaveBeenCalledWith();
  });

  it("appends console lines in order and ignores replays", () => {
    const qc = new QueryClient();
    qc.setQueryData(keys.console, { lines: [{ seq: 1, at: "", text: "a" }], gap: false });
    dispatch(qc, "console:line", { seq: 2, at: "", text: "b" });
    dispatch(qc, "console:line", { seq: 2, at: "", text: "b" });
    const data = qc.getQueryData<{ lines: { seq: number }[] }>(keys.console);
    expect(data?.lines.map((l) => l.seq)).toEqual([1, 2]);
  });

  it("task events also refresh the task's domain", () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    dispatch(qc, "task:changed", { id: "x", kind: "backup" });
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.backups });
  });
});

describe("connectEvents", () => {
  it("recreates the stream with a fresh URL (ticket) after an error", async () => {
    vi.useFakeTimers();
    const made: {
      url: string;
      es: { onopen?: () => void; onerror?: () => void; close: () => void; closed: boolean };
    }[] = [];
    class FakeES {
      onopen?: () => void;
      onerror?: () => void;
      closed = false;
      static CLOSED = 2;
      constructor(public url: string) {
        made.push({ url, es: this });
      }
      addEventListener() {}
      close() {
        this.closed = true;
      }
    }
    vi.stubGlobal("EventSource", FakeES);
    let n = 0;
    const states: string[] = [];
    const qc = new QueryClient();
    const stop = connectEvents(
      qc,
      (s) => states.push(s),
      async () => `/api/events?ticket=t${++n}`,
    );
    await vi.runOnlyPendingTimersAsync();
    expect(made.map((m) => m.url)).toEqual(["/api/events?ticket=t1"]);
    made[0].es.onopen?.();
    made[0].es.onerror?.();
    expect(made[0].es.closed).toBe(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(made.map((m) => m.url)).toEqual(["/api/events?ticket=t1", "/api/events?ticket=t2"]);
    stop();
    expect(made[1].es.closed).toBe(true);
    expect(states).toContain("open");
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });
});
