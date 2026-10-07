import { DownloadIcon, Loader2Icon, PlusIcon, ServerIcon, SettingsIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { AddServerDialog } from "@/components/AddServerDialog";
import { LaunchProvider } from "@/components/LaunchProvider";
import { ServerCard } from "@/components/ServerCard";
import { GameFolder, SettingsDialog } from "@/components/SettingsDialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { App as AppAPI, type Config, type Core, errorMessage, Launcher } from "@/lib/api";

type Update = { version: string; url: string };

export function App() {
  const { t } = useTranslation();
  const [servers, setServers] = useState<Config.Server[] | null>(null);
  const [game, setGame] = useState<Core.GameInfo | null>(null);
  const [version, setVersion] = useState("");
  const [update, setUpdate] = useState<Update | null>(null);
  const [updating, setUpdating] = useState(false);
  const [adding, setAdding] = useState(false);
  const [settings, setSettings] = useState(false);
  const [autoConnect, setAutoConnect] = useState(true);

  const reload = useCallback(async () => {
    try {
      setServers(await Launcher.Servers());
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    reload();
    Launcher.Game().then(setGame);
    Launcher.AutoConnect().then(setAutoConnect);
    AppAPI.Version().then(setVersion);
    AppAPI.CheckUpdate()
      .then((u) => u.available && setUpdate({ version: u.version, url: u.url }))
      .catch(() => {});
  }, [reload]);

  const applyUpdate = async () => {
    setUpdating(true);
    try {
      await AppAPI.ApplyUpdate();
    } catch (err) {
      toast.error(errorMessage(err));
      setUpdating(false);
    }
  };

  const toggleAutoConnect = async (on: boolean) => {
    setAutoConnect(on);
    try {
      await Launcher.SetAutoConnect(on);
    } catch (err) {
      setAutoConnect(!on);
      toast.error(errorMessage(err));
    }
  };

  return (
    <LaunchProvider onDone={reload}>
      <div className="mx-auto flex min-h-screen max-w-3xl flex-col gap-6 p-6">
        <header className="flex items-center justify-between gap-4">
          <div>
            <h1 className="font-semibold text-xl">{t("EasyPZ Launcher")}</h1>
            <p className="text-muted-foreground text-sm">{t("Sync mods and join Project Zomboid servers")}</p>
          </div>
          <div className="flex items-center gap-4">
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="flex items-center gap-2">
                  <Switch id="auto-connect" checked={autoConnect} onCheckedChange={toggleAutoConnect} />
                  <Label htmlFor="auto-connect">{t("Join automatically")}</Label>
                </div>
              </TooltipTrigger>
              <TooltipContent className="max-w-64">
                {autoConnect
                  ? t("Play syncs the mods, starts the game and joins the server.")
                  : t("Play syncs the mods and starts the game: join the server from the game's menu.")}
              </TooltipContent>
            </Tooltip>
            <div className="flex items-center gap-2">
              <Button variant="outline" onClick={() => setAdding(true)}>
                <PlusIcon />
                {t("Add server")}
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setSettings(true)}
                aria-label={t("Settings")}
              >
                <SettingsIcon />
              </Button>
            </div>
          </div>
        </header>

        {update && (
          <Alert>
            <DownloadIcon />
            <AlertTitle>{t("Launcher {{v}} is available", { v: update.version })}</AlertTitle>
            <AlertDescription className="flex flex-wrap items-center gap-2">
              <Button size="sm" onClick={applyUpdate} disabled={updating}>
                {updating && <Loader2Icon className="animate-spin" />}
                {t("Update and restart")}
              </Button>
              <Button size="sm" variant="link" onClick={() => AppAPI.OpenURL(update.url)}>
                {t("What's new")}
              </Button>
            </AlertDescription>
          </Alert>
        )}

        {game && !game.found && (
          <Alert>
            <AlertTitle>{t("Project Zomboid was not found")}</AlertTitle>
            <AlertDescription className="w-full pt-2">
              <GameFolder game={game} onChanged={setGame} />
            </AlertDescription>
          </Alert>
        )}

        {servers === null ? (
          <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
        ) : servers.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed p-10 text-center">
            <ServerIcon className="size-8 text-muted-foreground" />
            <p className="text-muted-foreground text-sm">
              {t("Add a server with its mod page link, or pick one you already joined from the game.")}
            </p>
            <Button onClick={() => setAdding(true)}>
              <PlusIcon />
              {t("Add server")}
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            {servers.map((s) => (
              <ServerCard
                key={s.id}
                server={s}
                gameVersion={game?.version ?? ""}
                autoConnect={autoConnect}
                onChanged={reload}
              />
            ))}
          </div>
        )}
      </div>

      <AddServerDialog
        open={adding}
        onOpenChange={setAdding}
        existing={servers ?? []}
        onAdded={() => {
          setAdding(false);
          reload();
        }}
      />
      <SettingsDialog
        open={settings}
        onOpenChange={setSettings}
        game={game}
        version={version}
        onGameChanged={setGame}
      />
    </LaunchProvider>
  );
}
