import { Biohazard, Lock, RotateCcw, Save } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, type Schemas, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useAction, useSandbox } from "@/api/queries";
import { Empty } from "@/components/Empty";
import { PageHeader } from "@/components/PageHeader";
import { SettingRow, useEdits } from "@/components/SettingRow";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

type Entry = Schemas["SandboxEntryView"];

// problem mirrors the server's checks so bad input is caught before saving.
export function problem(e: Entry, v: string): string | null {
  if (e.kind !== "int" && e.kind !== "float") return null;
  const n = v.trim() === "" ? Number.NaN : Number(v);
  if (!Number.isFinite(n)) return "Must be a number";
  if (e.kind === "int" && !Number.isInteger(n)) return "Must be a whole number";
  if ((e.min != null && n < e.min) || (e.max != null && n > e.max)) {
    return `Must be between ${e.min ?? "…"} and ${e.max ?? "…"}`;
  }
  return null;
}

// choices lists the options (the server adds back the one PZ's comments
// leave out, unlabelled) plus the current value if it is not among them.
export function choices(e: Entry, current: string): { value: string; label: string }[] {
  const describe = (v: string | number, label = "") => ({
    value: String(v),
    label: label ? `${v} — ${label}` : `${v} (not described)`,
  });
  const opts = (e.options ?? []).map((o) => describe(o.value, o.label));
  if (opts.length === 0) return opts;
  for (const v of [e.value, current]) {
    if (!opts.some((o) => o.value === v)) opts.push(describe(v));
  }
  return opts.sort((a, b) => Number(a.value) - Number(b.value));
}

function Field({ entry, value, onChange }: { entry: Entry; value: string; onChange: (v: string) => void }) {
  if (entry.readOnly) {
    return <p className="bg-muted rounded-md px-3 py-1.5 font-mono text-xs">{entry.value}</p>;
  }
  if (entry.kind === "bool") {
    return (
      <Switch
        checked={value === "true"}
        onCheckedChange={(v) => onChange(v ? "true" : "false")}
        aria-label={entry.key}
      />
    );
  }
  const opts = entry.kind === "int" ? choices(entry, value) : [];
  if (opts.length > 0) {
    return (
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger className="w-full max-w-72" aria-label={entry.key}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {opts.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  }
  const numeric = entry.kind !== "string";
  return (
    <Input
      value={value}
      inputMode={numeric ? "decimal" : undefined}
      aria-label={entry.key}
      aria-invalid={problem(entry, value) != null}
      className={cn("font-mono text-xs", numeric && "max-w-40")}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function defaultText(e: Entry): string | null {
  if (!e.default) return null;
  const opt = e.options?.find((o) => String(o.value) === e.default);
  return opt ? `${opt.value} — ${opt.label}` : e.default;
}

export default function Sandbox() {
  const { t } = useTranslation();
  const sandbox = useSandbox();
  const [filter, setFilter] = useState("");
  const { edits, set, reset, dirty } = useEdits();
  const save = useAction(() => unwrap(api.PUT("/config/sandbox", { body: { values: edits } })), {
    success: (r) => r.message,
    invalidate: [keys.config],
  });

  const entries = sandbox.data?.entries ?? [];
  const groups = useMemo(() => {
    const f = filter.toLowerCase();
    const out = new Map<string, Entry[]>();
    for (const e of entries) {
      if (f && !e.key.toLowerCase().includes(f) && !e.description.toLowerCase().includes(f)) continue;
      const dot = e.key.indexOf(".");
      const group = dot < 0 ? "" : e.key.slice(0, dot);
      out.set(group, [...(out.get(group) ?? []), e]);
    }
    return [...out];
  }, [entries, filter]);
  const invalid = entries.some((e) => e.key in edits && problem(e, edits[e.key]) != null);

  return (
    <div>
      <PageHeader
        title={t("Sandbox")}
        description={sandbox.data?.path}
        actions={
          <>
            <Input
              placeholder={t("Search options…")}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="w-56"
            />
            <Button variant="outline" disabled={!dirty} onClick={reset}>
              <RotateCcw /> {t("Discard")}
            </Button>
            <Button
              disabled={!dirty || invalid || save.isPending}
              onClick={() => save.mutate(undefined, { onSuccess: reset })}
            >
              <Save /> {t("Save")}
            </Button>
          </>
        }
      />
      <p className="text-muted-foreground mb-4 text-sm">
        {t(
          "Sandbox options are read when the server starts: changes apply to the world at the next restart.",
        )}
        {dirty > 0 && ` ${t("{{count}} unsaved change(s)", { count: dirty })}`}
      </p>
      {sandbox.isPending ? (
        <Card className="h-64 animate-pulse" />
      ) : !sandbox.data?.exists ? (
        <Empty icon={Biohazard} title={t("No sandbox file yet")}>
          {t("The game creates it on first start.")}
        </Empty>
      ) : (
        <div className="space-y-6">
          {groups.map(([group, list]) => (
            <Card key={group} className="gap-0 divide-y p-0">
              <CardHeader className="py-4">
                <CardTitle>{group || t("General")}</CardTitle>
              </CardHeader>
              {list.map((e) => {
                const value = edits[e.key] ?? e.value;
                const err = e.key in edits ? problem(e, value) : null;
                const def = defaultText(e);
                const range = e.min != null || e.max != null ? `${e.min ?? "…"} – ${e.max ?? "…"}` : null;
                return (
                  <SettingRow
                    key={e.key}
                    name={group ? e.key.slice(group.length + 1) : e.key}
                    changed={e.key in edits}
                    description={e.description}
                    badge={
                      e.readOnly && (
                        <Badge variant="secondary" className="gap-1">
                          <Lock className="size-3" /> {t("managed by the game")}
                        </Badge>
                      )
                    }
                    hint={
                      err ? (
                        <span className="text-destructive">{t(err)}</span>
                      ) : (
                        [
                          range && t("Range {{range}}", { range }),
                          def && t("Default {{value}}", { value: def }),
                        ]
                          .filter(Boolean)
                          .join(" · ") || undefined
                      )
                    }
                  >
                    <Field entry={e} value={value} onChange={(v) => set(e.key, e.value, v)} />
                  </SettingRow>
                );
              })}
            </Card>
          ))}
          {groups.length === 0 && (
            <p className="text-muted-foreground p-6 text-center text-sm">{t("No matching option.")}</p>
          )}
        </div>
      )}
    </div>
  );
}
