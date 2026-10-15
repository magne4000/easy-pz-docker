import {
  EllipsisVerticalIcon,
  ExternalLinkIcon,
  Gamepad2Icon,
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
import { EditServerDialog } from "@/components/EditServerDialog";
import { autoLogins, JoinSetup } from "@/components/JoinSetup";
import { useLaunch } from "@/components/LaunchProvider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { App, type Config, type Core, errorMessage, Launcher } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { useStatus } from "@/lib/hooks";
import { cn } from "@/lib/utils";

const statusStyles: Record<string, string> = {
  available: "bg-success/15 text-success border-success/30",
  restarting: "bg-warning/15 text-warning border-warning/30",
  unavailable: "bg-muted text-muted-foreground",
};

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
  const [settingUp, setSettingUp] = useState(false);
  const [editing, setEditing] = useState(false);

  const loading = !status && !error;
  // Joining needs a login saved in the game; without one, Play only starts the game.
  const accounts = autoLogins(status?.saved);
  const ready = accounts.length > 0;
  const preferred = accounts.find((a) => a.username === server.account) ?? accounts[0];
  const [choice, setChoice] = useState<string | null>(null);
  const selected = choice ?? (preferred ? String(preferred.id) : "");

  const remove = async () => {
    try {
      await Launcher.RemoveServer(server.id);
      onChanged();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  const mismatch = !!status?.gameVersion && !!gameVersion && status.gameVersion !== gameVersion;

  return (
    <Card className="gap-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <span className="truncate">{server.name}</span>
          <StatusBadge status={status} error={error} />
        </CardTitle>
        <CardDescription className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="font-mono">
            {!server.host || !server.port ? t("No address") : `${server.host}:${server.port}`}
          </span>
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
              {ready && (
                <DropdownMenuItem onSelect={() => launch.run("start", server)} disabled={launch.busy}>
                  <Gamepad2Icon />
                  {t("Start without joining")}
                </DropdownMenuItem>
              )}
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
          {ready ? (
            <>
              {accounts.length > 1 && (
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
                  </SelectContent>
                </Select>
              )}
              <Button
                onClick={() => launch.run("play", server, { accountId: Number(selected) })}
                disabled={launch.busy}
              >
                <PlayIcon />
                {t("Join")}
              </Button>
              {accounts.length === 1 && (
                <span className="text-muted-foreground text-sm">
                  {t("as {{user}}", { user: accounts[0].username })}
                </span>
              )}
            </>
          ) : (
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    <Button onClick={() => launch.run("start", server)} disabled={launch.busy || loading}>
                      <PlayIcon />
                      {t("Play")}
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {t("Syncs the mods and starts the game: join the server from the game's Multiplayer menu.")}
                </TooltipContent>
              </Tooltip>
              {!loading && (
                <Button variant="link" size="sm" className="px-0" onClick={() => setSettingUp(true)}>
                  {t("Set up auto-join")}
                </Button>
              )}
            </>
          )}
        </div>
      </CardContent>
      <Dialog open={settingUp} onOpenChange={setSettingUp}>
        <DialogContent className="sm:max-w-lg">
          <JoinSetup server={server} saved={status?.saved} onClose={() => setSettingUp(false)} />
        </DialogContent>
      </Dialog>
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
    const busy = !error && status?.busy;
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant="outline"
            className={
              busy ? statusStyles.restarting : "bg-destructive/15 text-destructive border-destructive/30"
            }
          >
            {busy ? t("Busy") : t("Unreachable")}
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
