import { FileCog, Lock, RotateCcw, Save } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useIni, usePaths, useServerStatus } from "@/api/queries";
import { Empty } from "@/components/Empty";
import { PageHeader } from "@/components/PageHeader";
import { SettingRow, useEdits } from "@/components/SettingRow";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

type Entry = Schemas["IniEntryView"];

function kindOf(value: string): "bool" | "number" | "text" {
  if (value === "true" || value === "false") return "bool";
  if (/^-?\d+(\.\d+)?$/.test(value)) return "number";
  return "text";
}

function Field({ entry, value, onChange }: { entry: Entry; value: string; onChange: (v: string) => void }) {
  const kind = kindOf(entry.value);
  if (entry.managed) {
    return (
      <p className="bg-muted rounded-md px-3 py-1.5 font-mono text-xs break-all">{entry.value || "—"}</p>
    );
  }
  if (kind === "bool") {
    return (
      <Switch
        checked={value === "true"}
        onCheckedChange={(v) => onChange(v ? "true" : "false")}
        aria-label={entry.key}
      />
    );
  }
  return (
    <Input
      value={value}
      inputMode={kind === "number" ? "decimal" : undefined}
      type={/password/i.test(entry.key) ? "password" : "text"}
      className={cn("font-mono text-xs", kind === "number" && "max-w-40")}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

export default function ServerConfig() {
  const { t } = useTranslation();
  const ini = useIni();
  const paths = usePaths();
  const status = useServerStatus();
  const [filter, setFilter] = useState("");
  const { edits, set, reset, dirty } = useEdits();
  const save = useAction(
    (reload: boolean) => unwrap(api.PUT("/config/ini", { body: { values: edits, reload } })),
    {
      success: (r) => r.message,
      invalidate: [keys.config],
    },
  );
  const entries = ini.data?.entries ?? [];
  const shown = useMemo(() => {
    const f = filter.toLowerCase();
    return entries.filter(
      (e) => !f || e.key.toLowerCase().includes(f) || e.comment.toLowerCase().includes(f),
    );
  }, [entries, filter]);
  const running = status.data?.state === "running";
  const commit = (reload: boolean) => save.mutate(reload, { onSuccess: reset });

  return (
    <div>
      <PageHeader
        title={t("Server config")}
        description={ini.data?.path}
        actions={
          <>
            <Input
              placeholder={t("Search settings…")}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="w-56"
            />
            <Button variant="outline" disabled={!dirty} onClick={reset}>
              <RotateCcw /> {t("Discard")}
            </Button>
            <Button variant="outline" disabled={!dirty || save.isPending} onClick={() => commit(false)}>
              <Save /> {t("Save")}
            </Button>
            <Button
              disabled={!dirty || !running || save.isPending}
              onClick={() => commit(true)}
              title={t("Save, then run reloadoptions on the running server")}
            >
              {t("Save & reload")}
            </Button>
          </>
        }
      />
      {dirty > 0 && (
        <p className="text-muted-foreground mb-4 text-sm">
          {t("{{count}} unsaved change(s)", { count: dirty })}
        </p>
      )}
      {ini.isPending ? (
        <Card className="h-64 animate-pulse" />
      ) : !ini.data?.exists ? (
        <Empty icon={FileCog} title={t("No server ini yet")}>
          {t("The game creates it on first start; pzman seeds ports, RCON and backup settings before that.")}
        </Empty>
      ) : (
        <Card className="divide-y p-0">
          {shown.map((e) => (
            <SettingRow
              key={e.key}
              name={e.key}
              changed={e.key in edits}
              description={e.comment}
              badge={
                e.managed && (
                  <Badge variant="secondary" className="gap-1">
                    <Lock className="size-3" /> {t("managed")}
                  </Badge>
                )
              }
            >
              <Field entry={e} value={edits[e.key] ?? e.value} onChange={(v) => set(e.key, e.value, v)} />
            </SettingRow>
          ))}
          {shown.length === 0 && (
            <p className="text-muted-foreground p-6 text-center text-sm">{t("No matching setting.")}</p>
          )}
        </Card>
      )}
      {paths.data && (
        <Card className="mt-6">
          <CardHeader>
            <CardTitle>{t("File locations")}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-2 text-sm sm:grid-cols-[max-content_1fr] sm:gap-x-6">
              {Object.entries(paths.data)
                .filter(([k]) => k !== "$schema")
                .map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt className="text-muted-foreground">{k}</dt>
                    <dd className="font-mono text-xs break-all">{String(v)}</dd>
                  </div>
                ))}
            </dl>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
