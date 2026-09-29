import { CalendarClock, Pencil, Play, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useSchedules } from "@/api/queries";
import { Confirm } from "@/components/Confirm";
import { Empty } from "@/components/Empty";
import { PageHeader } from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatDate, formatRelative, parseMinutes } from "@/lib/format";

type Input_ = Schemas["ScheduleInput"];
type Action = Input_["action"];

const PRESETS: { label: string; cron: string }[] = [
  { label: "Every day at 06:00", cron: "0 6 * * *" },
  { label: "Every 6 hours", cron: "0 */6 * * *" },
  { label: "Every hour", cron: "0 * * * *" },
  { label: "Every 30 minutes", cron: "*/30 * * * *" },
  { label: "Mondays at 04:00", cron: "0 4 * * 1" },
];

const empty: Input_ = {
  name: "",
  action: "restart",
  cron: "0 6 * * *",
  message: "",
  warnMinutes: [15, 5, 1],
  enabled: true,
};

function Editor({ value, onClose }: { value: (Input_ & { id?: number }) | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [form, setForm] = useState<Input_>(empty);
  const [warn, setWarn] = useState("");
  useEffect(() => {
    if (value) {
      setForm(value);
      setWarn((value.warnMinutes ?? []).join(", "));
    }
  }, [value]);
  const save = useAction(
    (v: Input_) =>
      value?.id
        ? unwrap(api.PUT("/schedules/{id}", { params: { path: { id: value.id } }, body: v }))
        : unwrap(api.POST("/schedules", { body: v })),
    { success: t("Schedule saved"), invalidate: [keys.schedules] },
  );
  const set = <K extends keyof Input_>(k: K, v: Input_[K]) => setForm((f) => ({ ...f, [k]: v }));
  const countdown = form.action === "restart" || form.action === "stop";
  return (
    <Dialog open={value !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{value?.id ? t("Edit schedule") : t("New schedule")}</DialogTitle>
          <DialogDescription>{t("Cron expressions use the panel's timezone.")}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="s-name">{t("Name")}</Label>
            <Input id="s-name" value={form.name} onChange={(e) => set("name", e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label>{t("Action")}</Label>
            <Select value={form.action} onValueChange={(v) => set("action", v as Action)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="restart">{t("Restart (with countdown)")}</SelectItem>
                <SelectItem value="stop">{t("Stop (with countdown)")}</SelectItem>
                <SelectItem value="save">{t("Save the world")}</SelectItem>
                <SelectItem value="broadcast">{t("Broadcast a message")}</SelectItem>
                <SelectItem value="backup">{t("Backup (if the world changed)")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="s-cron">{t("When (cron)")}</Label>
            <Input
              id="s-cron"
              className="font-mono"
              value={form.cron}
              onChange={(e) => set("cron", e.target.value)}
            />
            <div className="flex flex-wrap gap-1">
              {PRESETS.map((p) => (
                <Button
                  key={p.cron}
                  type="button"
                  size="sm"
                  variant="outline"
                  className="h-7 text-xs"
                  onClick={() => set("cron", p.cron)}
                >
                  {t(p.label)}
                </Button>
              ))}
            </div>
          </div>
          {(form.action === "broadcast" || countdown) && (
            <div className="grid gap-2">
              <Label htmlFor="s-msg">
                {form.action === "broadcast" ? t("Message") : t("Warning prefix")}
              </Label>
              <Input
                id="s-msg"
                value={form.message ?? ""}
                placeholder={countdown ? t("Server restart") : undefined}
                onChange={(e) => set("message", e.target.value)}
              />
            </div>
          )}
          {countdown && (
            <div className="grid gap-2">
              <Label htmlFor="s-warn">{t("Warn players (minutes before)")}</Label>
              <Input
                id="s-warn"
                value={warn}
                onChange={(e) => setWarn(e.target.value)}
                placeholder="15, 5, 1"
              />
              <p className="text-muted-foreground text-xs">
                {t("The largest value is the countdown length; empty means act immediately.")}
              </p>
            </div>
          )}
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={form.enabled} onCheckedChange={(v) => set("enabled", v)} /> {t("Enabled")}
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("Cancel")}
          </Button>
          <Button
            disabled={!form.name.trim() || save.isPending}
            onClick={() =>
              save.mutate(
                {
                  ...form,
                  message: form.message || undefined,
                  warnMinutes: countdown ? parseMinutes(warn) : [],
                },
                { onSuccess: onClose },
              )
            }
          >
            {t("Save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function Schedules() {
  const { t } = useTranslation();
  const q = useSchedules();
  const [editing, setEditing] = useState<(Input_ & { id?: number }) | null>(null);
  const del = useAction((id: number) => unwrap(api.DELETE("/schedules/{id}", { params: { path: { id } } })), {
    success: t("Schedule deleted"),
    invalidate: [keys.schedules],
  });
  const run = useAction(
    (id: number) => unwrap(api.POST("/schedules/{id}/run", { params: { path: { id } } })),
    {
      success: t("Started"),
      invalidate: [keys.schedules, keys.server],
    },
  );
  const toggle = useAction(
    (s: Schemas["Schedule"]) =>
      unwrap(
        api.PUT("/schedules/{id}", {
          params: { path: { id: s.id } },
          body: {
            name: s.name,
            action: s.action,
            cron: s.cron,
            message: s.message,
            warnMinutes: s.warnMinutes,
            enabled: !s.enabled,
          },
        }),
      ),
    { invalidate: [keys.schedules] },
  );
  const d = q.data;
  return (
    <div>
      <PageHeader
        title={t("Scheduler")}
        description={t("Timezone: {{tz}}", { tz: d?.timezone ?? "…" })}
        actions={
          <Button onClick={() => setEditing({ ...empty })}>
            <Plus /> {t("New schedule")}
          </Button>
        }
      />
      <div className="mb-6 grid gap-4 sm:grid-cols-2">
        <Card className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t("Automatic backup")}</CardDescription>
            <CardTitle className="text-lg">
              {d?.nextBackupAt ? formatRelative(d.nextBackupAt) : t("disabled")}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card className="gap-1 py-4">
          <CardHeader className="px-4">
            <CardDescription>{t("Next update check")}</CardDescription>
            <CardTitle className="text-lg">
              {d?.nextUpdateCheckAt ? formatRelative(d.nextUpdateCheckAt) : t("disabled")}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>
      {q.isPending ? (
        <Card className="h-40 animate-pulse" />
      ) : !d || (d.items ?? []).length === 0 ? (
        <Empty icon={CalendarClock} title={t("No schedules")}>
          {t("Add a nightly restart with countdown warnings, periodic saves or broadcasts.")}
        </Empty>
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("Enabled")}</TableHead>
                <TableHead>{t("Name")}</TableHead>
                <TableHead>{t("When")}</TableHead>
                <TableHead className="hidden md:table-cell">{t("Next run")}</TableHead>
                <TableHead className="hidden lg:table-cell">{t("Last run")}</TableHead>
                <TableHead className="text-right">{t("Actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(d.items ?? []).map((s) => (
                <TableRow key={s.id}>
                  <TableCell>
                    <Switch
                      checked={s.enabled}
                      onCheckedChange={() => toggle.mutate(s)}
                      aria-label={t("Enabled")}
                    />
                  </TableCell>
                  <TableCell>
                    <p className="font-medium">{s.name}</p>
                    <Badge variant="secondary" className="mt-1">
                      {t(s.action)}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{s.cron}</TableCell>
                  <TableCell className="hidden md:table-cell">
                    {s.enabled ? formatDate(s.nextRunAt) : "—"}
                  </TableCell>
                  <TableCell className="hidden lg:table-cell">
                    <p>{formatRelative(s.lastRunAt)}</p>
                    {s.lastResult && s.lastResult !== "ok" && (
                      <p className="text-destructive max-w-56 truncate text-xs" title={s.lastResult}>
                        {s.lastResult}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        title={t("Run now")}
                        onClick={() => run.mutate(s.id)}
                      >
                        <Play />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        title={t("Edit")}
                        onClick={() => setEditing({ ...s })}
                      >
                        <Pencil />
                      </Button>
                      <Confirm
                        trigger={
                          <Button variant="ghost" size="icon" title={t("Delete")}>
                            <Trash2 />
                          </Button>
                        }
                        title={t("Delete {{name}}?", { name: s.name })}
                        description={t("The schedule stops running immediately.")}
                        confirm={t("Delete")}
                        destructive
                        onConfirm={() => del.mutate(s.id)}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
      <Editor value={editing} onClose={() => setEditing(null)} />
    </div>
  );
}
