import { Loader2Icon } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { autoLogins, JoinSetup } from "@/components/JoinSetup";
import { useLaunch } from "@/components/LaunchProvider";
import { Button } from "@/components/ui/button";
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
import { type Config, errorMessage, Launcher, type Saved } from "@/lib/api";
import { useStatus } from "@/lib/hooks";
import { cn } from "@/lib/utils";

export function AddServerDialog({
  open,
  onOpenChange,
  existing,
  onAdded,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existing: Config.Server[];
  onAdded: () => void;
}) {
  // Mounted while open only, so each opening starts on a fresh form.
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <AddServer existing={existing} onAdded={onAdded} onClose={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

function AddServer({
  existing,
  onAdded,
  onClose,
}: {
  existing: Config.Server[];
  onAdded: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [url, setUrl] = useState("");
  const [saved, setSaved] = useState<Saved.Server[] | null>(null);
  const [picked, setPicked] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [added, setAdded] = useState<{ server: Config.Server; saved?: Saved.Server } | null>(null);

  useEffect(() => {
    Launcher.SavedServers()
      .then(setSaved)
      .catch((err) => {
        toast.error(errorMessage(err));
        setSaved([]);
      });
  }, []);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const link = url.trim();
      const srv =
        picked == null ? await Launcher.AddServerURL(link) : await Launcher.ImportSaved(picked, link);
      toast.success(t("Added {{name}}", { name: srv.name }));
      onAdded();
      const match = saved?.find((s) => s.ip === srv.host && s.port === srv.port);
      if (autoLogins(match).length > 0) {
        onClose();
      } else {
        setAdded({ server: srv, saved: match });
      }
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (added) {
    return <AddedSetup server={added.server} saved={added.saved} onClose={onClose} />;
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <DialogHeader>
        <DialogTitle>{t("Add a server")}</DialogTitle>
        <DialogDescription>
          {t("Paste the server's mod page link, or pick a server saved in the game's Multiplayer list.")}
        </DialogDescription>
      </DialogHeader>
      <div className="space-y-2">
        <Label htmlFor="add-url">{t("Mod page link")}</Label>
        <Input
          id="add-url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://pz.example.com/mods/…"
          autoFocus
          required={picked == null}
        />
        <p className="text-muted-foreground text-xs">
          {picked == null
            ? t("Ask the server admin for it.")
            : t("Optional: without it, the launcher joins the server but does not sync mods.")}
        </p>
      </div>
      <div className="space-y-2">
        <Label>{t("Saved in the game")}</Label>
        {saved === null ? (
          <Loader2Icon className="mx-auto my-3 size-5 animate-spin text-muted-foreground" />
        ) : saved.length === 0 ? (
          <p className="text-muted-foreground text-xs">{t("No server is saved in the game yet.")}</p>
        ) : (
          <div className="max-h-56 space-y-1 overflow-auto">
            {saved.map((s) => {
              const already = existing.some((e) => e.host === s.ip && e.port === s.port);
              const logins = autoLogins(s);
              return (
                <button
                  type="button"
                  key={s.id}
                  disabled={already}
                  aria-pressed={picked === s.id}
                  onClick={() => setPicked(picked === s.id ? null : s.id)}
                  className={cn(
                    "flex w-full items-center justify-between gap-3 rounded-md border px-3 py-2 text-left text-sm",
                    picked === s.id ? "border-ring bg-accent" : "hover:bg-accent/50",
                    already && "opacity-50",
                  )}
                >
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{s.name}</span>
                    <span className="block truncate font-mono text-muted-foreground text-xs">
                      {s.ip}:{s.port}
                    </span>
                  </span>
                  <span className="shrink-0 text-muted-foreground text-xs">
                    {already
                      ? t("Already added")
                      : logins.length === 0
                        ? t("Password not remembered")
                        : logins.length === 1
                          ? logins[0].username
                          : t("{{n}} accounts", { n: logins.length })}
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </div>
      <DialogFooter>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2Icon className="animate-spin" />}
          {t("Add")}
        </Button>
      </DialogFooter>
    </form>
  );
}

// Second step for a server the launcher cannot join yet; follows the game's saved accounts live.
function AddedSetup({
  server,
  saved,
  onClose,
}: {
  server: Config.Server;
  saved?: Saved.Server;
  onClose: () => void;
}) {
  const launch = useLaunch();
  const { status } = useStatus(server, launch.busy, launch.finished);
  return <JoinSetup server={server} saved={status ? status.saved : saved} onClose={onClose} />;
}
