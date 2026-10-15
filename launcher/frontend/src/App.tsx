import { Loader2Icon, PlusIcon, ServerIcon, SettingsIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { AddServerDialog } from "@/components/AddServerDialog";
import { LaunchProvider } from "@/components/LaunchProvider";
import { ServerCard } from "@/components/ServerCard";
import { GameFolder, SettingsDialog } from "@/components/SettingsDialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { App as AppAPI, type Config, type Core, errorMessage, Launcher } from "@/lib/api";

export function App() {
  const { t } = useTranslation();
  const [servers, setServers] = useState<Config.Server[] | null>(null);
  const [game, setGame] = useState<Core.GameInfo | null>(null);
  const [version, setVersion] = useState("");
  const [adding, setAdding] = useState(false);
  const [settings, setSettings] = useState(false);

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
    AppAPI.Version().then(setVersion);
  }, [reload]);

  return (
    <LaunchProvider onDone={reload}>
      <div className="mx-auto flex min-h-screen max-w-3xl flex-col gap-6 p-6">
        <header className="flex items-center justify-between gap-4">
          <div>
            <h1 className="font-semibold text-xl">{t("EasyPZ Launcher")}</h1>
            <p className="text-muted-foreground text-sm">{t("Sync mods and join Project Zomboid servers")}</p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => setAdding(true)}>
              <PlusIcon />
              {t("Add server")}
            </Button>
            <Button variant="ghost" size="icon" onClick={() => setSettings(true)} aria-label={t("Settings")}>
              <SettingsIcon />
            </Button>
          </div>
        </header>

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
              {t("Add a server with its mod page link, or pick one saved in the game.")}
            </p>
            <Button onClick={() => setAdding(true)}>
              <PlusIcon />
              {t("Add server")}
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            {servers.map((s) => (
              <ServerCard key={s.id} server={s} gameVersion={game?.version ?? ""} onChanged={reload} />
            ))}
          </div>
        )}
      </div>

      <AddServerDialog open={adding} onOpenChange={setAdding} existing={servers ?? []} onAdded={reload} />
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
