import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { keys } from "@/api/keys";
import { dispatch, eventActions } from "@/events";

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
