import { CheckCircle2Icon, FolderOpenIcon, TriangleAlertIcon } from "lucide-react";
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
      </DialogContent>
    </Dialog>
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
