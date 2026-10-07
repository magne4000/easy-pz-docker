import { Loader2Icon } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type Config, errorMessage, Launcher, type Saved } from "@/lib/api";
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
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("Add a server")}</DialogTitle>
          <DialogDescription>
            {t("Paste the server's mod page link to get its mods, or reuse a server saved in the game.")}
          </DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="url">
          <TabsList className="w-full">
            <TabsTrigger value="url">{t("Mod page link")}</TabsTrigger>
            <TabsTrigger value="saved">{t("Saved in the game")}</TabsTrigger>
          </TabsList>
          <TabsContent value="url">
            <ByURL onAdded={onAdded} />
          </TabsContent>
          <TabsContent value="saved">
            <FromGame open={open} existing={existing} onAdded={onAdded} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function ByURL({ onAdded }: { onAdded: () => void }) {
  const { t } = useTranslation();
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const srv = await Launcher.AddServerURL(url);
      toast.success(t("Added {{name}}", { name: srv.name }));
      setUrl("");
      onAdded();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 pt-2">
      <div className="space-y-2">
        <Label htmlFor="add-url">{t("Mod page link")}</Label>
        <Input
          id="add-url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://pz.example.com/mods/…"
          autoFocus
          required
        />
        <p className="text-muted-foreground text-xs">{t("Ask the server admin for it.")}</p>
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

function FromGame({
  open,
  existing,
  onAdded,
}: {
  open: boolean;
  existing: Config.Server[];
  onAdded: () => void;
}) {
  const { t } = useTranslation();
  const [saved, setSaved] = useState<Saved.Server[] | null>(null);
  const [picked, setPicked] = useState<number | null>(null);
  const [pageUrl, setPageUrl] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    Launcher.SavedServers()
      .then(setSaved)
      .catch((err) => {
        toast.error(errorMessage(err));
        setSaved([]);
      });
  }, [open]);

  const already = (s: Saved.Server) => existing.some((e) => e.host === s.ip && e.port === s.port);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (picked == null) return;
    setBusy(true);
    try {
      const srv = await Launcher.ImportSaved(picked, pageUrl.trim());
      toast.success(t("Added {{name}}", { name: srv.name }));
      setPicked(null);
      setPageUrl("");
      onAdded();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (saved === null) {
    return <Loader2Icon className="mx-auto my-6 size-5 animate-spin text-muted-foreground" />;
  }
  if (saved.length === 0) {
    return (
      <p className="py-6 text-center text-muted-foreground text-sm">
        {t("No server is saved in the game yet (non-Steam mode).")}
      </p>
    );
  }
  return (
    <form onSubmit={submit} className="space-y-4 pt-2">
      <div className="max-h-56 space-y-1 overflow-auto">
        {saved.map((s) => {
          const added = already(s);
          return (
            <button
              type="button"
              key={s.id}
              disabled={added}
              onClick={() => setPicked(s.id)}
              className={cn(
                "flex w-full items-center justify-between gap-3 rounded-md border px-3 py-2 text-left text-sm",
                picked === s.id ? "border-ring bg-accent" : "hover:bg-accent/50",
                added && "opacity-50",
              )}
            >
              <span className="min-w-0">
                <span className="block truncate font-medium">{s.name}</span>
                <span className="block truncate font-mono text-muted-foreground text-xs">
                  {s.ip}:{s.port}
                </span>
              </span>
              <span className="shrink-0 text-muted-foreground text-xs">
                {added
                  ? t("Already added")
                  : s.accounts.length === 1
                    ? s.accounts[0].username
                    : t("{{n}} accounts", { n: s.accounts.length })}
              </span>
            </button>
          );
        })}
      </div>
      <div className="space-y-2">
        <Label htmlFor="import-url">{t("Mod page link (optional)")}</Label>
        <Input
          id="import-url"
          value={pageUrl}
          onChange={(e) => setPageUrl(e.target.value)}
          placeholder="https://pz.example.com/mods/…"
        />
        <p className="text-muted-foreground text-xs">
          {t("Without it, the launcher joins the server but does not sync mods.")}
        </p>
      </div>
      <DialogFooter>
        <Button type="submit" disabled={busy || picked == null}>
          {busy && <Loader2Icon className="animate-spin" />}
          {t("Add")}
        </Button>
      </DialogFooter>
    </form>
  );
}
