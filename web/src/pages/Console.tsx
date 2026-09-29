import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, Eraser, Send } from "lucide-react";
import { type KeyboardEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { api, unwrap } from "@/api/client";
import { useAction, useConsole, useServerStatus } from "@/api/queries";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

const QUICK = ["players", "save", "help", "showoptions"];
const HISTORY_KEY = "pzman.rconHistory";

function loadHistory(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(HISTORY_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string").slice(-50) : [];
  } catch {
    return [];
  }
}

function lineClass(text: string) {
  if (text.startsWith("> ")) return "text-sky-600 dark:text-sky-400";
  if (/\b(ERROR|SEVERE|Exception|fatal)\b/i.test(text)) return "text-destructive";
  if (/\bWARN(ING)?\b/.test(text)) return "text-warning";
  if (text.startsWith("[steamcmd]") || text.startsWith("[pzman]")) return "text-muted-foreground";
  return "";
}

export default function Console() {
  const { t } = useTranslation();
  const consoleQ = useConsole();
  const status = useServerStatus();
  const [filter, setFilter] = useState("");
  const [clearedAt, setClearedAt] = useState(0);
  const [command, setCommand] = useState("");
  const [history, setHistory] = useState<string[]>(loadHistory);
  const [cursor, setCursor] = useState(-1);
  const [follow, setFollow] = useState(true);
  const scroller = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);

  const lines = useMemo(() => {
    const all = (consoleQ.data?.lines ?? []).filter((l) => l.seq > clearedAt);
    if (!filter) return all;
    const f = filter.toLowerCase();
    return all.filter((l) => l.text.toLowerCase().includes(f));
  }, [consoleQ.data, filter, clearedAt]);

  // Only the visible lines are in the DOM: the buffer holds up to CONSOLE_MAX.
  const virt = useVirtualizer({
    count: lines.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => 20,
    overscan: 20,
    getItemKey: (i) => lines[i].seq,
    scrollMargin: list.current?.offsetTop ?? 0,
  });

  useLayoutEffect(() => {
    if (follow && lines.length > 0) virt.scrollToIndex(lines.length - 1, { align: "end" });
  }, [lines, follow, virt]);

  useEffect(() => {
    try {
      localStorage.setItem(HISTORY_KEY, JSON.stringify(history.slice(-50)));
    } catch {
      // history is a convenience; ignore storage failures
    }
  }, [history]);

  const exec = useAction((cmd: string) => unwrap(api.POST("/console/rcon", { body: { command: cmd } })));

  const run = (cmd: string) => {
    const c = cmd.trim();
    if (!c) return;
    exec.mutate(c, {
      onSuccess: (r) => {
        if (r.rejected) toast.warning(r.message ?? t("Command rejected"));
      },
    });
    setHistory((h) => [...h.filter((x) => x !== c), c]);
    setCursor(-1);
    setCommand("");
    setFollow(true);
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") run(command);
    if (e.key === "ArrowUp" && history.length) {
      e.preventDefault();
      const next = cursor < 0 ? history.length - 1 : Math.max(0, cursor - 1);
      setCursor(next);
      setCommand(history[next]);
    }
    if (e.key === "ArrowDown" && cursor >= 0) {
      e.preventDefault();
      const next = cursor + 1;
      if (next >= history.length) {
        setCursor(-1);
        setCommand("");
      } else {
        setCursor(next);
        setCommand(history[next]);
      }
    }
  };

  const running = status.data?.state === "running";

  return (
    <div className="flex h-[calc(100svh-7.5rem)] flex-col md:h-[calc(100svh-9.5rem)]">
      <PageHeader
        title={t("Console")}
        description={t("Server log and RCON terminal.")}
        actions={
          <>
            <Input
              placeholder={t("Filter…")}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="w-48"
            />
            <Button
              variant="outline"
              size="icon"
              title={t("Clear view")}
              onClick={() => setClearedAt(consoleQ.data?.lines?.at(-1)?.seq ?? 0)}
            >
              <Eraser />
            </Button>
          </>
        }
      />
      <Card className="relative flex min-h-0 flex-1 flex-col gap-0 overflow-hidden p-0">
        <div
          ref={scroller}
          onScroll={(e) => {
            const el = e.currentTarget;
            setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
          }}
          className="flex-1 overflow-auto bg-slate-950 p-3 font-mono text-xs leading-5 text-slate-200"
        >
          {consoleQ.data?.gap && (
            <p className="text-slate-500">{t("… older lines were dropped from the buffer")}</p>
          )}
          {lines.length === 0 && <p className="text-slate-500">{t("No output yet.")}</p>}
          <div ref={list} className="relative w-full" style={{ height: virt.getTotalSize() }}>
            {virt.getVirtualItems().map((row) => {
              const l = lines[row.index];
              return (
                <div
                  key={row.key}
                  data-index={row.index}
                  ref={virt.measureElement}
                  className={cn(
                    "absolute top-0 left-0 w-full break-all whitespace-pre-wrap",
                    lineClass(l.text),
                  )}
                  style={{ transform: `translateY(${row.start - virt.options.scrollMargin}px)` }}
                >
                  <span className="mr-2 text-slate-600 select-none">
                    {new Date(l.at).toLocaleTimeString()}
                  </span>
                  {l.text}
                </div>
              );
            })}
          </div>
        </div>
        {!follow && (
          <Button
            size="sm"
            variant="secondary"
            className="absolute right-4 bottom-20"
            onClick={() => setFollow(true)}
          >
            <ArrowDown /> {t("Follow")}
          </Button>
        )}
        <div className="border-t p-3">
          <div className="mb-2 flex flex-wrap gap-1.5">
            {QUICK.map((q) => (
              <Button
                key={q}
                size="sm"
                variant="outline"
                className="h-7 font-mono text-xs"
                disabled={!running}
                onClick={() => run(q)}
              >
                {q}
              </Button>
            ))}
          </div>
          <div className="flex gap-2">
            <Input
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              onKeyDown={onKey}
              disabled={!running}
              placeholder={
                running ? t('RCON command, e.g. servermsg "hello"') : t("The server is not running")
              }
              className="font-mono"
              autoComplete="off"
            />
            <Button onClick={() => run(command)} disabled={!running || !command.trim() || exec.isPending}>
              <Send /> {t("Send")}
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}
