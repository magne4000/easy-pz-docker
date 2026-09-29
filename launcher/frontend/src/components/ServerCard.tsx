import {
  EllipsisVerticalIcon,
  ExternalLinkIcon,
  PencilIcon,
  PlayIcon,
  RefreshCwIcon,
  TrashIcon,
  TriangleAlertIcon,
  UsersIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { type Credentials, CredentialsDialog } from "@/components/CredentialsDialog";
import { EditServerDialog } from "@/components/EditServerDialog";
import { useLaunch } from "@/components/LaunchProvider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { App, type Config, type Core, errorMessage, Launcher, type Saved } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { useStatus } from "@/lib/hooks";
import { cn } from "@/lib/utils";

const OTHER = "__other__";

const statusStyles: Record<string, string> = {
  available: "bg-success/15 text-success border-success/30",
  restarting: "bg-warning/15 text-warning border-warning/30",
  unavailable: "bg-muted text-muted-foreground",
};

export function autoLoginAccounts(saved?: Saved.Server | null): Saved.Account[] {
  return (saved?.accounts ?? []).filter((a) => a.authType === 1 && a.savePassword);
}

export function ServerCard({
  server,
  gameVersion,
  onChanged,
}: {
  server: Config.Server;
  gameVersion: string;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const launch = useLaunch();
  const { status, error, refresh } = useStatus(server, launch.busy, launch.finished);
  const [askCreds, setAskCreds] = useState(false);
  const [editing, setEditing] = useState(false);

  const accounts = autoLoginAccounts(status?.saved);
  const preferred = accounts.find((a) => a.username === server.account) ?? accounts[0];
  const [choice, setChoice] = useState<string | null>(null);
  const selected = choice ?? (preferred ? String(preferred.id) : OTHER);

  const play = () => {
    if (selected !== OTHER) {
      launch.run("play", server, { accountId: Number(selected) });
    } else {
      setAskCreds(true);
    }
  };

  const playWith = (c: Credentials) => {
    setAskCreds(false);
    launch.run("play", server, {
      username: c.username,
      password: c.password,
      serverPassword: c.serverPassword,
    });
  };

  const remove = async () => {
    try {
      await Launcher.RemoveServer(server.id);
      onChanged();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const mismatch = !!status?.gameVersion && !!gameVersion && status.gameVersion !== gameVersion;
  const noAddress = !server.host || !server.port;

  return (
    <Card className="gap-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <span className="truncate">{server.name}</span>
          <StatusBadge status={status} error={error} />
        </CardTitle>
        <CardDescription className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="font-mono">{noAddress ? t("No address") : `${server.host}:${server.port}`}</span>
          {status?.players != null && (
            <span className="inline-flex items-center gap-1">
              <UsersIcon className="size-3.5" />
              {status.players}
            </span>
          )}
          {status?.gameVersion && <span>{t("Game {{v}}", { v: status.gameVersion })}</span>}
        </CardDescription>
        <CardAction>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={t("More")}>
                <EllipsisVerticalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => setEditing(true)}>
                <PencilIcon />
                {t("Edit")}
              </DropdownMenuItem>
              {server.pageUrl && (
                <DropdownMenuItem onSelect={() => App.OpenURL(server.pageUrl ?? "")}>
                  <ExternalLinkIcon />
                  {t("Open mod page")}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onSelect={refresh}>
                <RefreshCwIcon />
                {t("Refresh")}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={remove}>
                <TrashIcon />
                {t("Remove")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3">
        {mismatch && (
          <p className="flex items-center gap-2 text-sm text-warning">
            <TriangleAlertIcon className="size-4 shrink-0" />
            {t("The server runs game {{server}}, you have {{client}}: joining may fail.", {
              server: status?.gameVersion,
              client: gameVersion,
            })}
          </p>
        )}
        <ModsLine status={status} onSync={() => launch.run("sync", server)} disabled={launch.busy} />
        <div className="flex flex-wrap items-center gap-2">
          <Select value={selected} onValueChange={setChoice}>
            <SelectTrigger className="w-56" aria-label={t("Account")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {accounts.map((a) => (
                <SelectItem key={a.id} value={String(a.id)}>
                  {a.username}
                </SelectItem>
              ))}
              <SelectItem value={OTHER}>
                {accounts.length ? t("Other account…") : t("Enter a login…")}
              </SelectItem>
            </SelectContent>
          </Select>
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Button onClick={play} disabled={launch.busy || noAddress}>
                  <PlayIcon />
                  {t("Play")}
                </Button>
              </span>
            </TooltipTrigger>
            {noAddress && <TooltipContent>{t("Set the server's address first (Edit)")}</TooltipContent>}
          </Tooltip>
          <Button variant="outline" onClick={() => launch.run("start", server)} disabled={launch.busy}>
            {t("Start game")}
          </Button>
        </div>
      </CardContent>
      <CredentialsDialog
        key={askCreds ? "open" : "closed"}
        open={askCreds}
        onOpenChange={setAskCreds}
        serverName={server.name}
        defaultUsername={status?.saved?.accounts[0]?.username ?? ""}
        hasSavedServerPassword={!!status?.saved}
        onSubmit={playWith}
      />
      <EditServerDialog
        open={editing}
        onOpenChange={setEditing}
        server={server}
        onSaved={() => {
          setEditing(false);
          onChanged();
        }}
      />
    </Card>
  );
}

function StatusBadge({ status, error }: { status: Core.Status | null; error: string | null }) {
  const { t } = useTranslation();
  if (error || (status && !status.reachable)) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge variant="outline" className="bg-destructive/15 text-destructive border-destructive/30">
            {t("Unreachable")}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>{error ?? status?.error}</TooltipContent>
      </Tooltip>
    );
  }
  if (!status?.status) return null;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="outline" className={cn("gap-1.5 capitalize", statusStyles[status.status])}>
          <span
            className={cn(
              "size-1.5 rounded-full bg-current",
              status.status === "restarting" && "animate-pulse",
            )}
          />
          {t(status.status)}
        </Badge>
      </TooltipTrigger>
      {status.statusMessage && <TooltipContent>{status.statusMessage}</TooltipContent>}
    </Tooltip>
  );
}

function ModsLine({
  status,
  onSync,
  disabled,
}: {
  status: Core.Status | null;
  onSync: () => void;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  if (!status) return null;
  if (!status.hasModPage) {
    return (
      <p className="text-muted-foreground text-sm">
        {t("No mod page: mods are not synced for this server.")}
      </p>
    );
  }
  if (!status.reachable) return null;
  const todo = status.plan.download.length;
  return (
    <div className="flex items-center gap-3 text-sm">
      {todo === 0 ? (
        <span className="text-muted-foreground">
          {t("{{n}} mods, all up to date", { n: status.modCount })}
        </span>
      ) : (
        <>
          <span>
            {t("{{n}} of {{total}} mods to download", { n: todo, total: status.modCount })}
            {status.plan.bytes > 0 && (
              <span className="text-muted-foreground"> ({formatBytes(status.plan.bytes)})</span>
            )}
          </span>
          <Button variant="link" size="xs" className="h-auto px-0" onClick={onSync} disabled={disabled}>
            {t("Sync now")}
          </Button>
        </>
      )}
    </div>
  );
}
