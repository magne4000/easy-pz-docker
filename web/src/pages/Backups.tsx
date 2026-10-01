import { Archive, Download, FileText, History, Pin, PinOff, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useBackup, useBackups } from "@/api/queries";
import { Confirm } from "@/components/Confirm";
import { Empty } from "@/components/Empty";
import { PageHeader } from "@/components/PageHeader";
import { PauseEmptyWarning } from "@/components/PauseEmptyWarning";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate, formatRelative } from "@/lib/format";

function CreateDialog({ running }: { running: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState("");
  const [force, setForce] = useState(true);
  const create = useAction(() => unwrap(api.POST("/backups", { body: { note: note || undefined, force } })), {
    success: t("Backup started"),
    invalidate: [keys.backups, keys.tasks],
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button disabled={running}>
          <Plus /> {t("Back up now")}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("Manual backup")}</DialogTitle>
          <DialogDescription>
            {t(
              "Archives the world, the account database and the server config. Mods are not included; their versions are recorded.",
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="note">{t("Note")}</Label>
            <Input
              id="note"
              value={note}
              maxLength={200}
              onChange={(e) => setNote(e.target.value)}
              placeholder={t("Before the horde night")}
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={force} onCheckedChange={(v) => setForce(v === true)} />
            {t("Back up even if nothing changed since the last backup")}
          </label>
        </div>
        <DialogFooter>
          <Button
            disabled={create.isPending}
            onClick={() =>
              create.mutate(undefined, {
                onSuccess: () => {
                  setOpen(false);
                  setNote("");
                },
              })
            }
          >
            {t("Start backup")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ManifestDialog({ id, onClose }: { id: number | null; onClose: () => void }) {
  const { t } = useTranslation();
  const q = useBackup(id);
  const m = q.data?.manifest;
  return (
    <Dialog open={id !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {t("Backup")} #{id}
          </DialogTitle>
          <DialogDescription>{q.data?.backup.file}</DialogDescription>
        </DialogHeader>
        {q.isError && <p className="text-destructive text-sm">{(q.error as Error).message}</p>}
        {m && (
          <div className="space-y-4 text-sm">
            <dl className="grid grid-cols-2 gap-3">
              <div>
                <dt className="text-muted-foreground text-xs">{t("Created")}</dt>
                <dd>{formatDate(m.createdAt)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t("Game build")}</dt>
                <dd className="font-mono">{m.buildId || "—"}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t("Files")}</dt>
                <dd>
                  {m.files?.length ?? 0} · {formatBytes(m.fingerprint.totalSize)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t("Newest change")}</dt>
                <dd>{formatDate(m.fingerprint.newestMtime)}</dd>
              </div>
            </dl>
            <div>
              <p className="mb-1 font-medium">{t("Mods at the time")}</p>
              {(m.mods ?? []).length === 0 ? (
                <p className="text-muted-foreground">{t("none")}</p>
              ) : (
                <ul className="space-y-1 font-mono text-xs">
                  {m.mods?.map((r) => (
                    <li key={r.workshopId}>
                      {r.workshopId} · {r.modIds?.join(", ")} · {formatDate(r.timeUpdated)}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <p className="mb-1 font-medium">{t("Included paths")}</p>
              <ScrollArea className="h-40 rounded-md border p-2">
                <ul className="font-mono text-xs">
                  {m.files?.map((f) => (
                    <li key={f.path} className="flex justify-between gap-4">
                      <span className="truncate">{f.path}</span>
                      <span className="text-muted-foreground shrink-0">{formatBytes(f.size)}</span>
                    </li>
                  ))}
                </ul>
              </ScrollArea>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export default function Backups() {
  const { t } = useTranslation();
  const q = useBackups();
  const [details, setDetails] = useState<number | null>(null);
  const pin = useAction(
    (v: { id: number; pinned: boolean }) =>
      unwrap(api.PUT("/backups/{id}/pin", { params: { path: { id: v.id } }, body: { pinned: v.pinned } })),
    { invalidate: [keys.backups] },
  );
  const del = useAction((id: number) => unwrap(api.DELETE("/backups/{id}", { params: { path: { id } } })), {
    success: t("Backup deleted"),
    invalidate: [keys.backups],
  });
  const restore = useAction(
    (id: number) => unwrap(api.POST("/backups/{id}/restore", { params: { path: { id } } })),
    { success: t("Restore started"), invalidate: [keys.server, keys.tasks] },
  );
  const d = q.data;
  const p = d?.policy;
  return (
    <div>
      <PageHeader
        title={t("Backups")}
        description={t(
          "Snapshots are only taken when the world changed, so every archive is a distinct state.",
        )}
        actions={<CreateDialog running={!!d?.status.running} />}
      />
      <PauseEmptyWarning className="mb-6" />
      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <Card className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t("Stored")}</CardDescription>
            <CardTitle className="text-2xl">
              {d?.status.count ?? 0} · {formatBytes(d?.status.totalSize)}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-muted-foreground px-4 font-mono text-xs break-all">
            {d?.status.backupDir}
          </CardContent>
        </Card>
        <Card className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t("Next scheduled")}</CardDescription>
            <CardTitle className="text-2xl">
              {d?.nextRunAt ? formatRelative(d.nextRunAt) : t("disabled")}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-muted-foreground px-4 text-xs">
            {d?.status.running
              ? t("A backup is running…")
              : d?.status.unchanged
                ? t("World unchanged since the last backup: it will be skipped.")
                : t("The world changed since the last backup.")}
          </CardContent>
        </Card>
        <Card className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t("Retention")}</CardDescription>
            <CardTitle className="text-base">
              {p
                ? t("{{keep}} recent, {{daily}} daily, {{weekly}} weekly", {
                    keep: p.keep,
                    daily: p.keepDaily,
                    weekly: p.keepWeekly,
                  })
                : "—"}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-muted-foreground px-4 text-xs">
            {p?.maxTotalBytes
              ? t("Size budget {{size}}", { size: formatBytes(p.maxTotalBytes) })
              : t("No size budget")}{" "}
            · {t("pinned backups are never pruned")}
          </CardContent>
        </Card>
      </div>
      {q.isPending ? (
        <Card className="h-40 animate-pulse" />
      ) : !d || (d.items ?? []).length === 0 ? (
        <Empty icon={Archive} title={t("No backups yet")}>
          {t("Scheduled backups run automatically once the world has something to save.")}
        </Empty>
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("Created")}</TableHead>
                <TableHead>{t("Reason")}</TableHead>
                <TableHead className="hidden md:table-cell">{t("Build")}</TableHead>
                <TableHead className="text-right">{t("Size")}</TableHead>
                <TableHead className="text-right">{t("Actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(d.items ?? []).map((b) => (
                <TableRow key={b.id}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {b.pinned && <Pin className="text-primary size-3.5" />}
                      <span>{formatDate(b.createdAt)}</span>
                    </div>
                    {b.note && <p className="text-muted-foreground text-xs">{b.note}</p>}
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">{t(b.reason)}</Badge>
                  </TableCell>
                  <TableCell className="hidden font-mono text-xs md:table-cell">{b.buildId || "—"}</TableCell>
                  <TableCell className="text-right">{formatBytes(b.size)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        title={t("Details")}
                        onClick={() => setDetails(b.id)}
                      >
                        <FileText />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        title={b.pinned ? t("Unpin") : t("Pin (never prune)")}
                        onClick={() => pin.mutate({ id: b.id, pinned: !b.pinned })}
                      >
                        {b.pinned ? <PinOff /> : <Pin />}
                      </Button>
                      <Button variant="ghost" size="icon" asChild title={t("Download")}>
                        <a href={`/api/backups/${b.id}/download`} download>
                          <Download />
                        </a>
                      </Button>
                      <Confirm
                        trigger={
                          <Button variant="ghost" size="icon" title={t("Restore")}>
                            <History />
                          </Button>
                        }
                        title={t("Restore this backup?")}
                        description={
                          <>
                            <p>
                              {t(
                                "The server is stopped (players are disconnected), the current state is backed up, then this snapshot replaces the world, the account database and the server config.",
                              )}
                            </p>
                            <p className="mt-2 font-medium">{formatDate(b.createdAt)}</p>
                          </>
                        }
                        confirm={t("Restore")}
                        destructive
                        onConfirm={() => restore.mutate(b.id)}
                      />
                      <Confirm
                        trigger={
                          <Button variant="ghost" size="icon" title={t("Delete")}>
                            <Trash2 />
                          </Button>
                        }
                        title={t("Delete this backup?")}
                        description={t("The archive is removed from disk. This cannot be undone.")}
                        confirm={t("Delete")}
                        destructive
                        onConfirm={() => del.mutate(b.id)}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
      <ManifestDialog id={details} onClose={() => setDetails(null)} />
    </div>
  );
}
