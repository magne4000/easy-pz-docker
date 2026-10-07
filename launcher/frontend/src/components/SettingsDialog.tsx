import { CheckCircle2Icon, DownloadIcon, FolderOpenIcon, Loader2Icon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { App, type Core, errorMessage, Launcher } from "@/lib/api";

export function SettingsDialog({
  open,
  onOpenChange,
  game,
  version,
  onGameChanged,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  game: Core.GameInfo | null;
  version: string;
  onGameChanged: (g: Core.GameInfo) => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("Settings")}</DialogTitle>
          <DialogDescription>{t("EasyPZ Launcher {{v}}", { v: version })}</DialogDescription>
        </DialogHeader>
        <GameFolder game={game} onChanged={onGameChanged} />
        <LauncherUpdate />
      </DialogContent>
    </Dialog>
  );
}

type Check =
  | { state: "checking" }
  | { state: "latest" }
  | { state: "available"; version: string; url: string }
  | { state: "error"; message: string };

// Nothing is checked or installed until the player asks.
function LauncherUpdate() {
  const { t } = useTranslation();
  const [check, setCheck] = useState<Check | null>(null);
  const [updating, setUpdating] = useState(false);

  const run = async () => {
    setCheck({ state: "checking" });
    try {
      const u = await App.CheckUpdate();
      setCheck(u.available ? { state: "available", version: u.version, url: u.url } : { state: "latest" });
    } catch (err) {
      setCheck({ state: "error", message: errorMessage(err) });
    }
  };

  const apply = async () => {
    setUpdating(true);
    try {
      await App.ApplyUpdate();
    } catch (err) {
      toast.error(errorMessage(err));
      setUpdating(false);
    }
  };

  return (
    <div className="space-y-2">
      <Label>{t("Updates")}</Label>
      {check?.state === "available" ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm">{t("Launcher {{v}} is available", { v: check.version })}</span>
          <Button size="sm" onClick={apply} disabled={updating}>
            {updating ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
            {t("Update and restart")}
          </Button>
          <Button size="sm" variant="link" onClick={() => App.OpenURL(check.url)}>
            {t("What's new")}
          </Button>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" size="sm" onClick={run} disabled={check?.state === "checking"}>
            {check?.state === "checking" && <Loader2Icon className="animate-spin" />}
            {t("Check for updates")}
          </Button>
          {check?.state === "latest" && (
            <span className="flex items-center gap-1.5 text-muted-foreground text-xs">
              <CheckCircle2Icon className="size-3.5 text-success" />
              {t("You have the latest version.")}
            </span>
          )}
          {check?.state === "error" && <span className="text-muted-foreground text-xs">{check.message}</span>}
        </div>
      )}
    </div>
  );
}

export function GameFolder({
  game,
  onChanged,
}: {
  game: Core.GameInfo | null;
  onChanged: (g: Core.GameInfo) => void;
}) {
  const { t } = useTranslation();

  const browse = async () => {
    try {
      const dir = await App.ChooseFolder(t("Choose the Project Zomboid folder"));
      if (!dir) return;
      onChanged(await Launcher.SetGameDir(dir));
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  return (
    <div className="min-w-0 space-y-2">
      <Label>{t("Project Zomboid folder")}</Label>
      <div className="flex items-center gap-2">
        <div
          className="min-w-0 flex-1 truncate rounded-md border px-3 py-2 font-mono text-sm"
          title={game?.dir}
        >
          {game?.dir || <span className="text-muted-foreground">{t("Not found")}</span>}
        </div>
        <Button variant="outline" onClick={browse}>
          <FolderOpenIcon />
          {t("Browse")}
        </Button>
      </div>
      {game?.found ? (
        <p className="flex items-center gap-1.5 text-muted-foreground text-xs">
          <CheckCircle2Icon className="size-3.5 text-success" />
          {game.detected ? t("Found automatically") : t("Chosen manually")}
          {game.version && ` · ${t("game {{v}}", { v: game.version })}`}
        </p>
      ) : (
        <p className="flex items-center gap-1.5 text-warning text-xs">
          <TriangleAlertIcon className="size-3.5" />
          {t("Pick the folder that contains the game (the Steam copy works too).")}
        </p>
      )}
    </div>
  );
}
