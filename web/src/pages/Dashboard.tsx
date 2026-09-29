import {
  AlertTriangle,
  Download,
  ExternalLink,
  HardDrive,
  Megaphone,
  Play,
  RefreshCw,
  RotateCw,
  Save,
  Square,
  Users,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useServerStatus, useSystem, useTasks, useUpdates } from "@/api/queries";
import { PageHeader } from "@/components/PageHeader";
import { StatusBadge } from "@/components/StatusBadge";
import { TaskList } from "@/components/TaskList";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { formatBytes, formatDate, formatDuration, formatRelative } from "@/lib/format";
import { cn } from "@/lib/utils";

type Lifecycle = "stop" | "restart";

function LifecycleDialog({ kind, onClose }: { kind: Lifecycle | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [minutes, setMinutes] = useState("0");
  const [message, setMessage] = useState("");
  const act = useAction(
    (k: Lifecycle) =>
      unwrap(
        api.POST(k === "stop" ? "/server/stop" : "/server/restart", {
          body: { countdownMinutes: Number(minutes), message: message || undefined },
        }),
      ),
    {
      success: (r) => (r.message ? r.message : t("Request accepted")),
      invalidate: [keys.server, keys.tasks],
    },
  );
  return (
    <Dialog open={kind !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{kind === "stop" ? t("Stop the server") : t("Restart the server")}</DialogTitle>
          <DialogDescription>
            {t("The world is saved first. With a countdown, players are warned in-game before it happens.")}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label>{t("Countdown")}</Label>
            <Select value={minutes} onValueChange={setMinutes}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">{t("Now")}</SelectItem>
                {[1, 5, 15, 30].map((m) => (
                  <SelectItem key={m} value={String(m)}>
                    {t("{{count}} minutes", { count: m })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {minutes !== "0" && (
            <div className="grid gap-2">
              <Label htmlFor="lc-msg">{t("Warning prefix")}</Label>
              <Input
                id="lc-msg"
                placeholder={kind === "stop" ? t("Server shutdown") : t("Server restart")}
                value={message}
                onChange={(e) => setMessage(e.target.value)}
              />
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("Cancel")}
          </Button>
          <Button
            variant={kind === "stop" ? "destructive" : "default"}
            disabled={act.isPending}
            onClick={() => kind && act.mutate(kind, { onSuccess: onClose })}
          >
            {kind === "stop" ? t("Stop") : t("Restart")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function BroadcastDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const [message, setMessage] = useState("");
  const send = useAction((m: string) => unwrap(api.POST("/server/broadcast", { body: { message: m } })), {
    success: t("Message sent"),
  });
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("Broadcast a message")}</DialogTitle>
          <DialogDescription>{t("Shown to every connected player.")}</DialogDescription>
        </DialogHeader>
        <Textarea value={message} maxLength={500} onChange={(e) => setMessage(e.target.value)} />
        <DialogFooter>
          <Button
            disabled={!message.trim() || send.isPending}
            onClick={() =>
              send.mutate(message, {
                onSuccess: () => {
                  setMessage("");
                  onClose();
                },
              })
            }
          >
            <Megaphone /> {t("Send")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ServerCard({ s }: { s: Schemas["ServerView"] }) {
  const { t } = useTranslation();
  const [lifecycle, setLifecycle] = useState<Lifecycle | null>(null);
  const [broadcast, setBroadcast] = useState(false);
  const start = useAction(() => unwrap(api.POST("/server/start")), {
    success: t("Starting the server"),
    invalidate: [keys.server],
  });
  const save = useAction(() => unwrap(api.POST("/server/save")), { success: t("World saved") });
  const cancel = useAction(() => unwrap(api.POST("/server/cancel")), {
    success: (r) => (r.cancelled ? t("Countdown cancelled") : t("Nothing to cancel")),
    invalidate: [keys.server],
  });
  const running = s.state === "running";
  const stopped = s.state === "stopped" || s.state === "crashed";
  const busy = !!s.operation;

  return (
    <Card className="lg:col-span-2">
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div>
          <CardTitle className="flex items-center gap-3">
            {s.serverName} <StatusBadge state={s.state} />
          </CardTitle>
          <CardDescription className="mt-1">
            {s.nonSteam ? t("Non-Steam mode") : t("Steam mode")}
            {s.buildId && ` · ${t("build")} ${s.buildId}`}
            {s.branch && ` · ${s.branch}`}
          </CardDescription>
        </div>
        <Badge variant="outline" className="capitalize">
          {t(s.availability)}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-5">
        {s.crashLoop && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{t("Crash loop detected")}</AlertTitle>
            <AlertDescription>
              {t(
                "The server crashed repeatedly and automatic restarts were paused. Check the console, then start it manually.",
              )}
            </AlertDescription>
          </Alert>
        )}
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label={t("Uptime")} value={running ? formatDuration(s.uptimeSeconds) : "—"} />
          <Stat
            label={t("Players")}
            value={
              s.players.state === "stopped"
                ? "—"
                : s.players.state === "unknown"
                  ? t("unknown")
                  : String(s.players.count ?? 0)
            }
          />
          <Stat label={t("PID")} value={s.pid ? String(s.pid) : "—"} />
          <Stat
            label={t("Last exit")}
            value={s.lastExit ? t(s.lastExit.classification) : "—"}
            hint={s.lastExit ? `${s.lastExit.message} · ${formatDate(s.lastExit.at)}` : undefined}
            danger={s.lastExit?.classification === "crash" || s.lastExit?.classification === "start-failed"}
          />
        </dl>
        {(s.players.names ?? []).length > 0 && (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Users className="text-muted-foreground size-4" />
            {(s.players.names ?? []).map((n) => (
              <Badge key={n} variant="secondary">
                {n}
              </Badge>
            ))}
          </div>
        )}
        {busy && (
          <div className="bg-muted flex items-center justify-between gap-2 rounded-md px-3 py-2 text-sm">
            <span>
              {t("In progress")}: <strong>{t(s.operation)}</strong> · {formatRelative(s.operationSince)}
            </span>
            {(s.operation === "restart" || s.operation === "stop") && (
              <Button size="sm" variant="outline" onClick={() => cancel.mutate()}>
                {t("Cancel countdown")}
              </Button>
            )}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {stopped ? (
            <Button onClick={() => start.mutate()} disabled={busy || start.isPending}>
              <Play /> {t("Start")}
            </Button>
          ) : (
            <>
              <Button variant="outline" onClick={() => setLifecycle("restart")} disabled={busy}>
                <RotateCw /> {t("Restart")}
              </Button>
              <Button variant="outline" onClick={() => setLifecycle("stop")} disabled={busy}>
                <Square /> {t("Stop")}
              </Button>
            </>
          )}
          <Button variant="outline" onClick={() => save.mutate()} disabled={!running || save.isPending}>
            <Save /> {t("Save world")}
          </Button>
          <Button variant="outline" onClick={() => setBroadcast(true)} disabled={!running}>
            <Megaphone /> {t("Broadcast")}
          </Button>
        </div>
      </CardContent>
      <LifecycleDialog kind={lifecycle} onClose={() => setLifecycle(null)} />
      <BroadcastDialog open={broadcast} onClose={() => setBroadcast(false)} />
    </Card>
  );
}

function Stat({
  label,
  value,
  hint,
  danger,
}: {
  label: string;
  value: string;
  hint?: string;
  danger?: boolean;
}) {
  return (
    <div title={hint}>
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className={cn("mt-1 text-lg font-semibold capitalize", danger && "text-destructive")}>{value}</dd>
    </div>
  );
}

const windowLabels: Record<string, string> = {
  idle: "No update in progress",
  waiting: "Waiting for the server to be empty",
  saving: "Saving the world",
  stopping: "Stopping the server",
  backup: "Taking a pre-update backup",
  updating: "Updating",
  reconciling: "Reconciling mods",
  starting: "Starting the server",
  failed: "Update failed",
};

function UpdatesCard() {
  const { t } = useTranslation();
  const u = useUpdates();
  const check = useAction(() => unwrap(api.POST("/updates/check")), {
    success: (r) =>
      r.gameUpdateAvailable || (r.modUpdates?.length ?? 0) > 0
        ? t("Updates available")
        : t("Everything is up to date"),
    invalidate: [keys.updates],
  });
  const apply = useAction((force: boolean) => unwrap(api.POST("/updates/apply", { body: { force } })), {
    success: t("Update window opened"),
    invalidate: [keys.updates],
  });
  const cancel = useAction(() => unwrap(api.POST("/updates/cancel")), {
    success: t("Update window cancelled"),
    invalidate: [keys.updates],
  });
  const d = u.data;
  const w = d?.window;
  const active = w && w.state !== "idle" && w.state !== "failed";
  const mods = d?.modUpdates?.length ?? 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("Updates")}</CardTitle>
        <CardDescription>
          {t("Last checked")} {formatRelative(d?.lastCheckedAt)}
          {d?.nextCheckAt && ` · ${t("next")} ${formatRelative(d.nextCheckAt)}`}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt className="text-muted-foreground text-xs">{t("Installed build")}</dt>
            <dd className="font-mono">{d?.installedBuild || "—"}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground text-xs">{t("Latest build")}</dt>
            <dd className={cn("font-mono", d?.gameUpdateAvailable && "text-warning font-semibold")}>
              {d?.latestBuild || "—"}
            </dd>
          </div>
        </dl>
        <div className="flex flex-wrap gap-2 text-sm">
          {d?.gameUpdateAvailable && (
            <Badge className="bg-warning/15 text-warning">{t("Game update available")}</Badge>
          )}
          {mods > 0 && (
            <Badge className="bg-warning/15 text-warning">
              {t("{{count}} mod updates", { count: mods })}
            </Badge>
          )}
          {d && !d.gameUpdateAvailable && mods === 0 && <Badge variant="secondary">{t("Up to date")}</Badge>}
        </div>
        {d?.checkError && <p className="text-destructive text-xs">{d.checkError}</p>}
        {w && w.state !== "idle" && (
          <div
            className={cn("rounded-md border p-3 text-sm", w.state === "failed" && "border-destructive/50")}
          >
            <p className="font-medium">{t(windowLabels[w.state] ?? w.state)}</p>
            {w.message && <p className="text-muted-foreground text-xs">{w.message}</p>}
            {w.error && <p className="text-destructive text-xs">{w.error}</p>}
            {w.state === "waiting" && w.forceAt && (
              <p className="text-muted-foreground mt-1 text-xs">
                {t("Forced at")} {formatDate(w.forceAt)} ({formatRelative(w.forceAt)})
              </p>
            )}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => check.mutate()} disabled={check.isPending}>
            <RefreshCw className={cn(check.isPending && "animate-spin")} /> {t("Check now")}
          </Button>
          {w?.state === "waiting" ? (
            <>
              <Button size="sm" onClick={() => apply.mutate(true)} disabled>
                <Download /> {t("Waiting…")}
              </Button>
              <Button size="sm" variant="outline" onClick={() => cancel.mutate()}>
                {t("Cancel")}
              </Button>
            </>
          ) : (
            !active && (
              <>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => apply.mutate(false)}
                  disabled={apply.isPending}
                >
                  {t("Update when empty")}
                </Button>
                <Button size="sm" onClick={() => apply.mutate(true)} disabled={apply.isPending}>
                  <Download /> {t("Update now")}
                </Button>
              </>
            )
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function DisksCard() {
  const { t } = useTranslation();
  const sys = useSystem();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <HardDrive className="size-4" /> {t("Disk space")}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {(sys.data?.disks ?? []).map((d) => (
          <div key={d.label} className="space-y-1">
            <div className="flex justify-between text-sm">
              <span className="font-medium capitalize">{t(d.label)}</span>
              <span
                className={cn(
                  d.level === "critical"
                    ? "text-destructive"
                    : d.level === "warning"
                      ? "text-warning"
                      : "text-muted-foreground",
                )}
              >
                {d.error
                  ? t("unavailable")
                  : `${formatBytes(d.free)} ${t("free")} · ${d.usedPercent.toFixed(0)}%`}
              </span>
            </div>
            <Progress
              value={d.usedPercent}
              className={cn(
                "h-2",
                d.level === "critical" && "[&>div]:bg-destructive",
                d.level === "warning" && "[&>div]:bg-warning",
              )}
            />
            <p className="text-muted-foreground truncate font-mono text-xs" title={d.path}>
              {d.path}
            </p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

export default function Dashboard() {
  const { t } = useTranslation();
  const status = useServerStatus();
  const tasks = useTasks();
  const sys = useSystem();
  const s = status.data;
  return (
    <div>
      <PageHeader title={t("Dashboard")} description={t("Server state, updates and recent activity.")} />
      {sys.data?.diskLevel === "critical" && (
        <Alert variant="destructive" className="mb-6">
          <HardDrive />
          <AlertTitle>{t("A volume is almost full")}</AlertTitle>
          <AlertDescription>
            {t("Backups and saves may fail. Free some space or lower the backup retention.")}
          </AlertDescription>
        </Alert>
      )}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {s ? <ServerCard s={s} /> : <Card className="h-64 animate-pulse lg:col-span-2" />}
        <UpdatesCard />
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>{t("Recent activity")}</CardTitle>
          </CardHeader>
          <CardContent>
            <TaskList tasks={tasks.data?.items ?? []} limit={8} />
          </CardContent>
        </Card>
        <div className="space-y-6">
          <DisksCard />
          {sys.data?.modsPagePath && (
            <Card>
              <CardHeader>
                <CardTitle>{t("Player mod page")}</CardTitle>
                <CardDescription>
                  {t("Unlisted link for your players to download the exact mod set.")}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button asChild variant="outline" size="sm">
                  <a href={sys.data.modsPagePath} target="_blank" rel="noreferrer">
                    <ExternalLink /> {t("Open")}
                  </a>
                </Button>
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
