import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ExternalLink,
  FolderSync,
  Layers,
  ListOrdered,
  Package,
  Plus,
  RefreshCw,
  Trash2,
  Wand2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useConflicts, useMods, useSettings } from "@/api/queries";
import { Confirm } from "@/components/Confirm";
import { Empty } from "@/components/Empty";
import { PageHeader } from "@/components/PageHeader";
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
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { formatBytes, formatDate } from "@/lib/format";

// parseWorkshopIds accepts bare ids, Workshop URLs (?id=123) and any separators.
function parseWorkshopIds(text: string): string[] {
  const ids = new Set<string>();
  for (const m of text.matchAll(/[?&]id=(\d{4,20})/g)) ids.add(m[1]);
  const withoutUrls = text.replace(/https?:\/\/\S+/g, " ");
  for (const m of withoutUrls.matchAll(/\b(\d{4,20})\b/g)) ids.add(m[1]);
  return [...ids];
}

const workshopUrl = (id: string) => `https://steamcommunity.com/sharedfiles/filedetails/?id=${id}`;

function AddDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const ids = parseWorkshopIds(text);
  const add = useAction((workshopIds: string[]) => unwrap(api.POST("/mods", { body: { workshopIds } })), {
    success: (r) =>
      r.added?.length
        ? t("Added {{count}} item(s); downloading", { count: r.added.length })
        : t("Already tracked"),
    invalidate: [keys.mods, keys.tasks],
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus /> {t("Add mods")}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("Add Workshop items")}</DialogTitle>
          <DialogDescription>
            {t(
              "Paste Workshop ids or links, one per line. They are downloaded right away and load at the next restart.",
            )}
          </DialogDescription>
        </DialogHeader>
        <Textarea
          rows={6}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={"2392709985\nhttps://steamcommunity.com/sharedfiles/filedetails/?id=2169435993"}
          className="font-mono text-xs"
        />
        <p className="text-muted-foreground text-xs">
          {t("{{count}} id(s) recognised", { count: ids.length })}
        </p>
        <DialogFooter>
          <Button
            disabled={ids.length === 0 || add.isPending}
            onClick={() =>
              add.mutate(ids, {
                onSuccess: () => {
                  setText("");
                  setOpen(false);
                },
              })
            }
          >
            {t("Add")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CollectionDialog() {
  const { t } = useTranslation();
  const settings = useSettings();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  useEffect(() => {
    if (open) setText(settings.data?.workshopCollection ?? "");
  }, [open, settings.data?.workshopCollection]);
  const id = parseWorkshopIds(text)[0] ?? "";
  const imp = useAction(
    (collectionId: string) => unwrap(api.POST("/mods/collection", { body: { collectionId } })),
    {
      success: (r) => t("Imported {{count}} new item(s)", { count: r.added?.length ?? 0 }),
      invalidate: [keys.mods, keys.tasks],
    },
  );
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">
          <FolderSync /> {t("Import collection")}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("Import a Workshop collection")}</DialogTitle>
          <DialogDescription>
            {t("Every item of the collection is tracked. Items already tracked are left as they are.")}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="col">{t("Collection id or link")}</Label>
          <Input id="col" value={text} onChange={(e) => setText(e.target.value)} className="font-mono" />
        </div>
        <DialogFooter>
          <Button
            disabled={!id || imp.isPending}
            onClick={() => imp.mutate(id, { onSuccess: () => setOpen(false) })}
          >
            {t("Import")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ConflictsDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const q = useConflicts(open);
  const list = q.data?.conflicts ?? [];
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">
          <Layers /> {t("Conflicts")}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("File conflicts")}</DialogTitle>
          <DialogDescription>
            {t(
              "Files provided by more than one enabled mod. The mod loaded last wins, so check the load order.",
            )}
          </DialogDescription>
        </DialogHeader>
        {q.isPending ? (
          <p className="text-muted-foreground text-sm">{t("Scanning…")}</p>
        ) : list.length === 0 ? (
          <p className="text-sm">{t("No conflicts between enabled mods.")}</p>
        ) : (
          <ScrollArea className="max-h-96">
            <ul className="space-y-2 pr-3">
              {list.map((c) => (
                <li key={c.path} className="rounded-md border p-2">
                  <p className="font-mono text-xs break-all">{c.path}</p>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {(c.mods ?? []).map((m) => (
                      <Badge key={m} variant="secondary">
                        {m}
                      </Badge>
                    ))}
                  </div>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ItemCard({ item }: { item: Schemas["ItemView"] }) {
  const { t } = useTranslation();
  const toggle = useAction(
    (v: { modId: string; enabled: boolean }) =>
      unwrap(api.POST("/mods/toggle", { body: { workshopId: item.workshopId, ...v } })),
    { invalidate: [keys.mods] },
  );
  const remove = useAction(
    () => unwrap(api.DELETE("/mods/{workshopId}", { params: { path: { workshopId: item.workshopId } } })),
    { success: t("Removed"), invalidate: [keys.mods] },
  );
  return (
    <Card className="gap-3 py-4">
      <CardHeader className="flex flex-row items-start gap-4 px-4">
        {item.previewUrl ? (
          <img
            src={item.previewUrl}
            alt=""
            className="size-14 shrink-0 rounded-md object-cover"
            loading="lazy"
          />
        ) : (
          <div className="bg-muted flex size-14 shrink-0 items-center justify-center rounded-md">
            <Package className="text-muted-foreground size-6" />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <CardTitle className="flex flex-wrap items-center gap-2 text-base">
            <a
              href={workshopUrl(item.workshopId)}
              target="_blank"
              rel="noreferrer"
              className="hover:underline"
            >
              {item.title}
            </a>
            <ExternalLink className="text-muted-foreground size-3.5" />
            {!item.installed && <Badge variant="outline">{t("not downloaded")}</Badge>}
            {item.installed && item.updateAvailable && (
              <Badge className="bg-warning/15 text-warning">{t("update available")}</Badge>
            )}
          </CardTitle>
          <CardDescription className="mt-1 font-mono text-xs">
            {item.workshopId} · {formatBytes(item.size)} · {t("installed")}{" "}
            {formatDate(item.installedUpdated)}
            {item.remoteUpdated && ` · ${t("workshop")} ${formatDate(item.remoteUpdated)}`}
          </CardDescription>
          {item.error && <p className="text-destructive mt-1 text-xs">{item.error}</p>}
        </div>
        <Confirm
          trigger={
            <Button variant="ghost" size="icon" aria-label={t("Remove")}>
              <Trash2 />
            </Button>
          }
          title={t("Remove {{title}}?", { title: item.title })}
          description={t(
            "It is untracked, unlinked, removed from Mods= and its download is deleted. It stays loaded until the next restart.",
          )}
          confirm={t("Remove")}
          destructive
          onConfirm={() => remove.mutate()}
        />
      </CardHeader>
      {(item.mods ?? []).length > 0 && (
        <CardContent className="space-y-2 px-4">
          {(item.mods ?? []).map((m) => (
            <div key={m.modId} className="flex items-start gap-3 rounded-md border px-3 py-2">
              <Switch
                checked={m.enabled}
                onCheckedChange={(enabled) => toggle.mutate({ modId: m.modId, enabled })}
                aria-label={t("Enable {{mod}}", { mod: m.modId })}
              />
              <div className="min-w-0 flex-1 text-sm">
                <p className="font-medium">
                  {m.name || m.modId}{" "}
                  <span className="text-muted-foreground font-mono text-xs">{m.modId}</span>
                </p>
                {(m.requires ?? []).length > 0 && (
                  <p className="text-muted-foreground text-xs">
                    {t("requires")} {m.requires?.join(", ")}
                  </p>
                )}
                {m.enabled && (m.missing ?? []).length > 0 && (
                  <p className="text-destructive text-xs">
                    {t("missing")}: {m.missing?.join(", ")}
                  </p>
                )}
                {(m.maps ?? []).length > 0 && (
                  <p className="text-muted-foreground text-xs">
                    {t("maps")}: {m.maps?.join(", ")}
                  </p>
                )}
              </div>
            </div>
          ))}
        </CardContent>
      )}
    </Card>
  );
}

function LoadOrder({ order }: { order: string[] }) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(order);
  useEffect(() => setDraft(order), [order]);
  const dirty = draft.join(";") !== order.join(";");
  const save = useAction((modIds: string[]) => unwrap(api.PUT("/mods/order", { body: { modIds } })), {
    success: t("Load order saved"),
    invalidate: [keys.mods],
  });
  const autosort = useAction(() => unwrap(api.POST("/mods/autosort")), {
    success: t("Sorted by dependencies"),
    invalidate: [keys.mods],
  });
  const move = (i: number, d: number) => {
    const next = [...draft];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    setDraft(next);
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ListOrdered className="size-4" /> {t("Load order")}
        </CardTitle>
        <CardDescription>{t("Requirements are always loaded before the mods needing them.")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {draft.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t("No enabled mods.")}</p>
        ) : (
          <ol className="space-y-1">
            {draft.map((id, i) => (
              <li key={id} className="flex items-center gap-2 rounded-md border px-2 py-1 text-sm">
                <span className="text-muted-foreground w-6 text-right text-xs">{i + 1}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{id}</span>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7"
                  disabled={i === 0}
                  onClick={() => move(i, -1)}
                  aria-label={t("Move up")}
                >
                  <ArrowUp />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7"
                  disabled={i === draft.length - 1}
                  onClick={() => move(i, 1)}
                  aria-label={t("Move down")}
                >
                  <ArrowDown />
                </Button>
              </li>
            ))}
          </ol>
        )}
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => autosort.mutate()} disabled={autosort.isPending}>
            <Wand2 /> {t("Auto-sort")}
          </Button>
          {dirty && (
            <Button size="sm" onClick={() => save.mutate(draft)} disabled={save.isPending}>
              {t("Save order")}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export default function Mods() {
  const { t } = useTranslation();
  const mods = useMods();
  const check = useAction(() => unwrap(api.POST("/mods/check")), {
    success: (r) =>
      r.updates?.length
        ? t("{{count}} item(s) need an update", { count: r.updates.length })
        : t("All mods are up to date"),
    invalidate: [keys.mods, keys.updates],
  });
  const apply = useAction(() => unwrap(api.POST("/updates/apply", { body: { force: false } })), {
    success: t("The server restarts as soon as it is empty"),
    invalidate: [keys.updates],
  });
  const ov = mods.data;
  const missing = useMemo(
    () =>
      (ov?.items ?? []).flatMap((i) =>
        (i.mods ?? []).filter((m) => m.enabled && (m.missing ?? []).length > 0),
      ),
    [ov],
  );
  const needsUpdate = (ov?.items ?? []).filter((i) => i.updateAvailable).length;

  return (
    <div>
      <PageHeader
        title={t("Mods")}
        description={
          ov?.nonSteam
            ? t(
                "Non-Steam mode: pzman downloads Workshop items and links them into the server's mods folder.",
              )
            : t("Steam mode: Workshop items are listed in WorkshopItems= and Mods=.")
        }
        actions={
          <>
            <Button variant="outline" onClick={() => check.mutate()} disabled={check.isPending}>
              <RefreshCw className={check.isPending ? "animate-spin" : undefined} /> {t("Check updates")}
            </Button>
            <ConflictsDialog />
            <CollectionDialog />
            <AddDialog />
          </>
        }
      />
      <div className="mb-6 space-y-3">
        {ov?.restartRequired && (
          <Alert>
            <AlertTriangle />
            <AlertTitle>{t("Restart required")}</AlertTitle>
            <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
              <span>{t("The running server loaded a different mod set than the one configured now.")}</span>
              <Button size="sm" variant="outline" onClick={() => apply.mutate()} disabled={apply.isPending}>
                {t("Restart when empty")}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {needsUpdate > 0 && (
          <Alert>
            <RefreshCw />
            <AlertTitle>{t("{{count}} Workshop item(s) have updates", { count: needsUpdate })}</AlertTitle>
            <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
              <span>
                {t(
                  "Mods are updated inside the update window, with the server stopped, so files always match what the server loaded.",
                )}
              </span>
              <Button size="sm" variant="outline" onClick={() => apply.mutate()} disabled={apply.isPending}>
                {t("Update when empty")}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        {missing.length > 0 && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{t("Missing requirements")}</AlertTitle>
            <AlertDescription>
              {missing.map((m) => `${m.modId} → ${(m.missing ?? []).join(", ")}`).join(" · ")}
            </AlertDescription>
          </Alert>
        )}
        {(ov?.cycles?.length ?? 0) > 0 && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{t("Dependency cycle")}</AlertTitle>
            <AlertDescription>{ov?.cycles?.map((c) => (c ?? []).join(" → ")).join(" · ")}</AlertDescription>
          </Alert>
        )}
      </div>
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-2">
          {mods.isPending ? (
            <Card className="h-40 animate-pulse" />
          ) : ov?.items && ov.items.length > 0 ? (
            ov.items.map((it) => <ItemCard key={it.workshopId} item={it} />)
          ) : (
            <Empty icon={Package} title={t("No mods yet")}>
              {t("Add Workshop items by id or link, or import a collection.")}
            </Empty>
          )}
        </div>
        <div className="space-y-6">
          <LoadOrder order={ov?.loadOrder ?? []} />
          <Card>
            <CardHeader>
              <CardTitle>{t("Written to the ini")}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 font-mono text-xs break-all">
              <p>
                <span className="text-muted-foreground">Mods=</span>
                {(ov?.loadOrder ?? []).join(";")}
              </p>
              <p>
                <span className="text-muted-foreground">Map=</span>
                {(ov?.maps ?? []).join(";")}
              </p>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
