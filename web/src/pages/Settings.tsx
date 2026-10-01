import { Copy, ExternalLink, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useSettings, useSystem } from "@/api/queries";
import { PageHeader } from "@/components/PageHeader";
import { PauseEmptyWarning } from "@/components/PauseEmptyWarning";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { parseMinutes } from "@/lib/format";

type S = Schemas["Settings"];

function NumberField({
  id,
  label,
  hint,
  value,
  onChange,
  step = 1,
}: {
  id: string;
  label: string;
  hint?: string;
  value: number;
  onChange: (v: number) => void;
  step?: number;
}) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        min={0}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="max-w-40"
      />
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  );
}

export default function Settings() {
  const { t } = useTranslation();
  const q = useSettings();
  const sys = useSystem();
  const [form, setForm] = useState<S | null>(null);
  const [warn, setWarn] = useState("");
  useEffect(() => {
    if (q.data) {
      setForm(q.data);
      setWarn((q.data.warnMinutes ?? []).join(", "));
    }
  }, [q.data]);
  const save = useAction((s: S) => unwrap(api.PUT("/settings", { body: s })), {
    success: t("Settings saved"),
    invalidate: [keys.settings, keys.system],
  });
  if (!form) return <Card className="h-64 animate-pulse" />;
  const set = <K extends keyof S>(k: K, v: S[K]) => setForm((f) => (f ? { ...f, [k]: v } : f));
  const pageUrl = sys.data?.modsPagePath ? `${window.location.origin}${sys.data.modsPagePath}` : "";

  return (
    <div>
      <PageHeader
        title={t("Settings")}
        description={t("Panel behaviour. Game options live in Server config.")}
        actions={
          <Button
            onClick={() => save.mutate({ ...form, warnMinutes: parseMinutes(warn) })}
            disabled={save.isPending}
          >
            <Save /> {t("Save")}
          </Button>
        }
      />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>{t("Backups")}</CardTitle>
            <CardDescription>
              {t("A backup only happens when the world changed since the previous one.")}
            </CardDescription>
          </CardHeader>
          <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <PauseEmptyWarning className="sm:col-span-2" />
            <NumberField
              id="bi"
              label={t("Every (minutes)")}
              hint={t("0 disables scheduled backups")}
              value={form.backupIntervalMinutes}
              onChange={(v) => set("backupIntervalMinutes", v)}
            />
            <NumberField
              id="bk"
              label={t("Keep recent")}
              value={form.backupKeep}
              onChange={(v) => set("backupKeep", Math.max(1, v))}
            />
            <NumberField
              id="bd"
              label={t("Then keep daily")}
              hint={t("one per day, for this many days")}
              value={form.backupKeepDaily}
              onChange={(v) => set("backupKeepDaily", v)}
            />
            <NumberField
              id="bw"
              label={t("Then keep weekly")}
              hint={t("one per week, for this many weeks")}
              value={form.backupKeepWeekly}
              onChange={(v) => set("backupKeepWeekly", v)}
            />
            <NumberField
              id="bs"
              label={t("Size budget (GB)")}
              hint={t("0 means unlimited; the oldest go first")}
              step={0.5}
              value={form.backupMaxTotalGB}
              onChange={(v) => set("backupMaxTotalGB", v)}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t("Updates")}</CardTitle>
            <CardDescription>
              {t(
                "Game and Workshop updates share one window: announce, wait for an empty server, then update and restart.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <NumberField
              id="uc"
              label={t("Check every (minutes)")}
              hint={t("0 disables checks")}
              value={form.updateCheckMinutes}
              onChange={(v) => set("updateCheckMinutes", v)}
            />
            <NumberField
              id="um"
              label={t("Force after (minutes)")}
              hint={t("restart even with players online")}
              value={form.updateMaxDelayMinutes}
              onChange={(v) => set("updateMaxDelayMinutes", v)}
            />
            <div className="grid gap-1.5">
              <Label htmlFor="wm">{t("Warn players (minutes before)")}</Label>
              <Input
                id="wm"
                value={warn}
                onChange={(e) => setWarn(e.target.value)}
                placeholder="30, 15, 5, 1"
              />
            </div>
            <label className="flex items-center gap-2 self-end pb-2 text-sm">
              <Switch checked={form.autoUpdate} onCheckedChange={(v) => set("autoUpdate", v)} />
              {t("Update automatically")}
            </label>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t("Workshop")}</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-1.5">
            <Label htmlFor="col">{t("Collection id")}</Label>
            <Input
              id="col"
              className="max-w-60 font-mono"
              value={form.workshopCollection}
              onChange={(e) => set("workshopCollection", e.target.value.replace(/\D/g, ""))}
            />
            <p className="text-muted-foreground text-xs">
              {t("Shown on the player mod page, and pre-filled when importing.")}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t("Player mod page")}</CardTitle>
            <CardDescription>
              {t(
                "An unlisted, read-only page where players download the exact mod set (non-Steam clients). Not indexed by search engines.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={form.publicModsPage} onCheckedChange={(v) => set("publicModsPage", v)} />
              {t("Enabled")}
            </label>
            {pageUrl && q.data?.publicModsPage && (
              <div className="flex gap-2">
                <Input readOnly value={pageUrl} className="font-mono text-xs" />
                <Button
                  variant="outline"
                  size="icon"
                  title={t("Copy")}
                  onClick={() =>
                    navigator.clipboard.writeText(pageUrl).then(() => toast.success(t("Link copied")))
                  }
                >
                  <Copy />
                </Button>
                <Button variant="outline" size="icon" asChild title={t("Open")}>
                  <a href={pageUrl} target="_blank" rel="noreferrer">
                    <ExternalLink />
                  </a>
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>{t("About")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm sm:grid-cols-[max-content_1fr] sm:gap-x-6">
              <dt className="text-muted-foreground">{t("Version")}</dt>
              <dd>{sys.data?.version}</dd>
              <dt className="text-muted-foreground">{t("Mode")}</dt>
              <dd>{sys.data?.nonSteam ? t("Non-Steam (-Dzomboid.steam=0)") : t("Steam")}</dd>
              <dt className="text-muted-foreground">{t("Branch")}</dt>
              <dd>{sys.data?.branch || "public"}</dd>
              <dt className="text-muted-foreground">PUID / PGID</dt>
              <dd>
                {sys.data?.puid} / {sys.data?.pgid}
              </dd>
              <dt className="text-muted-foreground">{t("Timezone")}</dt>
              <dd>{sys.data?.timezone}</dd>
            </dl>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
