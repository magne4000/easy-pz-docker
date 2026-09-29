import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { Schemas } from "@/api/client";
import { keys } from "@/api/keys";
import type { operations } from "@/api/schema";

type Stream = operations["stream-events"]["responses"][200]["content"]["text/event-stream"][number];
export type EventName = Stream["event"];

type Action = { invalidate: QueryKey[] } | "console" | "all";

// The dispatch table from server events to query invalidations. Typed as a Record over the generated union, so a
// new server event is a compile error here until it is mapped.
export const eventActions: Record<EventName, Action> = {
  "server:status": { invalidate: [keys.server, keys.mods] },
  "console:line": "console",
  "task:changed": { invalidate: [keys.tasks] },
  "backups:changed": { invalidate: [keys.backups] },
  "mods:changed": { invalidate: [keys.mods] },
  "update:window": { invalidate: [keys.updates, keys.server] },
  "schedules:changed": { invalidate: [keys.schedules] },
  "disk:status": { invalidate: [keys.system] },
  "config:changed": { invalidate: [keys.config, keys.mods] },
  "settings:changed": { invalidate: [keys.settings, keys.system, keys.backups] },
  "stream:desync": "all",
};

const taskKinds: Record<string, QueryKey[]> = {
  backup: [keys.backups],
  restore: [keys.backups, keys.server],
  workshop: [keys.mods],
  "game-update": [keys.updates, keys.server],
  "update-check": [keys.updates, keys.mods],
};

type ConsoleCache = Schemas["ConsoleHistoryOutputBody"];
const CONSOLE_MAX = 5000;

function appendConsole(qc: QueryClient, line: Schemas["ConsoleLine"]) {
  qc.setQueryData<ConsoleCache>(keys.console, (old) => {
    if (!old) return old;
    const lines = old.lines ?? [];
    const last = lines[lines.length - 1];
    if (last && line.seq <= last.seq) return old;
    const next = [...lines, line];
    return { ...old, lines: next.length > CONSOLE_MAX ? next.slice(-CONSOLE_MAX) : next };
  });
}

export function dispatch(qc: QueryClient, name: string, data: unknown) {
  const action = eventActions[name as EventName];
  if (!action) return;
  if (action === "all") {
    qc.invalidateQueries();
    return;
  }
  if (action === "console") {
    appendConsole(qc, data as Schemas["ConsoleLine"]);
    return;
  }
  for (const key of action.invalidate) qc.invalidateQueries({ queryKey: key });
  if (name === "task:changed") {
    const kind = (data as Schemas["TaskChanged"]).kind;
    for (const key of taskKinds[kind] ?? []) qc.invalidateQueries({ queryKey: key });
  }
}

export type ConnectionState = "connecting" | "open" | "closed";

// connectEvents opens the single EventSource. The browser reconnects on its
// own; on every (re)open we refetch everything, since events may have been missed.
export function connectEvents(qc: QueryClient, onState: (s: ConnectionState) => void): () => void {
  const es = new EventSource("/api/events", { withCredentials: true });
  let opened = false;
  onState("connecting");
  es.onopen = () => {
    onState("open");
    if (opened) qc.invalidateQueries();
    opened = true;
  };
  es.onerror = () => onState(es.readyState === EventSource.CLOSED ? "closed" : "connecting");
  for (const name of Object.keys(eventActions)) {
    es.addEventListener(name, (ev) => {
      let data: unknown = null;
      try {
        data = JSON.parse((ev as MessageEvent).data);
      } catch {
        return;
      }
      dispatch(qc, name, data);
    });
  }
  return () => es.close();
}
