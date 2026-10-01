import type { CancellablePromise } from "@wailsio/runtime";
import { Events } from "@wailsio/runtime";
import { CheckCircle2Icon, Loader2Icon } from "lucide-react";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { type Config, Core, errorMessage, foreignMods, isCancelled, Launcher } from "@/lib/api";
import { formatBytes } from "@/lib/format";

type Kind = "play" | "start" | "sync";

type Job = {
  kind: Kind;
  server: Config.Server;
  req: Core.PlayRequest;
};

type LaunchContextValue = {
  busy: boolean;
  finished: number;
  run: (kind: Kind, server: Config.Server, req?: Partial<Core.PlayRequest>) => void;
};

const LaunchContext = createContext<LaunchContextValue>({ busy: false, finished: 0, run: () => {} });

export const useLaunch = () => useContext(LaunchContext);

function call(job: Job): CancellablePromise<void> {
  switch (job.kind) {
    case "play":
      return Launcher.Play(job.server.id, job.req);
    case "start":
      return Launcher.StartGame(job.server.id, job.req.takeOver);
    case "sync":
      return Launcher.Sync(job.server.id, job.req.takeOver);
  }
}

export function LaunchProvider({ children, onDone }: { children: ReactNode; onDone: () => void }) {
  const { t } = useTranslation();
  const [job, setJob] = useState<Job | null>(null);
  const [phase, setPhase] = useState<Core.Phase | null>(null);
  const [progress, setProgress] = useState<Core.SyncProgress | null>(null);
  const [foreign, setForeign] = useState<{ job: Job; folders: string[] } | null>(null);
  const pending = useRef<CancellablePromise<void> | null>(null);
  const [finished, setFinished] = useState(0);

  useEffect(() => {
    const offPhase = Events.On("play:phase", (ev) => setPhase(ev.data));
    const offSync = Events.On("sync:progress", (ev) => setProgress(ev.data));
    return () => {
      offPhase();
      offSync();
    };
  }, []);

  const start = useCallback(
    async (next: Job) => {
      setJob(next);
      setPhase(null);
      setProgress(null);
      const p = call(next);
      pending.current = p;
      try {
        await p;
        if (next.kind === "sync") {
          toast.success(t("Mods are up to date"));
        } else {
          setPhase(new Core.Phase({ serverId: next.server.id, phase: "launched" }));
          await new Promise((r) => setTimeout(r, 1200));
        }
      } catch (err) {
        const folders = foreignMods(err);
        if (folders) {
          setForeign({ job: next, folders });
        } else if (!isCancelled(err)) {
          toast.error(errorMessage(err));
        }
      } finally {
        pending.current = null;
        setJob(null);
        setFinished((n) => n + 1);
        onDone();
      }
    },
    [onDone, t],
  );

  const run = useCallback(
    (kind: Kind, server: Config.Server, req?: Partial<Core.PlayRequest>) => {
      if (pending.current) return;
      start({ kind, server, req: new Core.PlayRequest(req) });
    },
    [start],
  );

  const cancel = () => pending.current?.cancel();

  return (
    <LaunchContext.Provider value={{ busy: job !== null, finished, run }}>
      {children}
      <Dialog open={job !== null}>
        <DialogContent showCloseButton={false} onEscapeKeyDown={(e) => e.preventDefault()}>
          {job && (
            <>
              <DialogHeader>
                <DialogTitle>{job.server.name}</DialogTitle>
                <DialogDescription>{describe(t, job.kind, phase)}</DialogDescription>
              </DialogHeader>
              <LaunchBody phase={phase} progress={progress} />
              {phase?.phase !== "launched" && (
                <div className="flex justify-end">
                  <Button variant="outline" onClick={cancel}>
                    {t("Cancel")}
                  </Button>
                </div>
              )}
            </>
          )}
        </DialogContent>
      </Dialog>
      <AlertDialog open={foreign !== null} onOpenChange={(open) => !open && setForeign(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("Replace existing mods?")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                "These mod folders are already in your mods folder but were not installed by the launcher. The server's version will replace them:",
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ul className="max-h-40 overflow-auto rounded-md border p-2 font-mono text-sm">
            {foreign?.folders.map((f) => (
              <li key={f}>{f}</li>
            ))}
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("Cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (!foreign) return;
                const { job: prev } = foreign;
                setForeign(null);
                start({ ...prev, req: new Core.PlayRequest({ ...prev.req, takeOver: true }) });
              }}
            >
              {t("Replace")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </LaunchContext.Provider>
  );
}

function describe(t: (s: string) => string, kind: Kind, phase: Core.Phase | null): string {
  switch (phase?.phase) {
    case "waiting":
      return phase.message
        ? `${t("Waiting for the server")}: ${phase.message}`
        : t("Waiting for the server to be up");
    case "launching":
      return t("Starting Project Zomboid…");
    case "launched":
      return kind === "play"
        ? t("The game is starting and will join the server.")
        : t("The game is starting.");
    default:
      return t("Syncing mods…");
  }
}

function LaunchBody({ phase, progress }: { phase: Core.Phase | null; progress: Core.SyncProgress | null }) {
  if (phase?.phase === "launched") {
    return (
      <div className="flex items-center gap-2 text-success">
        <CheckCircle2Icon className="size-5" />
      </div>
    );
  }
  if ((!phase || phase.phase === "syncing") && progress) {
    const pct = progress.size > 0 ? Math.min(100, (progress.bytes / progress.size) * 100) : 0;
    return (
      <div className="space-y-2">
        <div className="flex justify-between gap-4 text-sm">
          <span className="truncate">{progress.title}</span>
          <span className="shrink-0 text-muted-foreground tabular-nums">
            {progress.index + 1} / {progress.total}
          </span>
        </div>
        <Progress value={pct} />
        <div className="text-muted-foreground text-xs tabular-nums">
          {formatBytes(progress.bytes)}
          {progress.size > 0 && ` / ${formatBytes(progress.size)}`}
        </div>
      </div>
    );
  }
  return <Loader2Icon className="size-5 animate-spin text-muted-foreground" />;
}
