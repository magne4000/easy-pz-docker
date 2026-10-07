import { Check, ChevronRight, Copy, Download, ExternalLink, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Schemas } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { formatBytes, formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";

type Pack = Schemas["PublicPack"];
type PublicData = Schemas["PublicData"];
type Launcher = Schemas["PublicLauncher"];

// The sed/PowerShell one-liners. They only replace existing
// lines, which is why the page also shows the full lines as a fallback.
export function posixCommand(d: Pick<PublicData, "modsLine" | "mapLine" | "iniName">) {
  const esc = (s: string) => s.replace(/[\\/&|]/g, "\\$&").replace(/'/g, "'\\''");
  return `sed -i.bak -E 's|^Mods=.*|Mods=${esc(d.modsLine)}|; s|^WorkshopItems=.*|WorkshopItems=|; s|^Map=.*|Map=${esc(d.mapLine)}|' 'Server/${d.iniName}'`;
}

export function powershellCommand(d: Pick<PublicData, "modsLine" | "mapLine" | "iniName">) {
  const lit = (s: string) => s.replace(/'/g, "''").replace(/\$/g, "$$$$");
  const f = `Server\\${d.iniName}`;
  return `Copy-Item '${f}' '${f}.bak'; (Get-Content '${f}') -replace '^Mods=.*','Mods=${lit(d.modsLine)}' -replace '^WorkshopItems=.*','WorkshopItems=' -replace '^Map=.*','Map=${lit(d.mapLine)}' | Set-Content '${f}'`;
}

function CopyBlock({ text, label }: { text: string; label?: string }) {
  const { t } = useTranslation();
  const [done, setDone] = useState(false);
  return (
    <div className="bg-muted relative rounded-md">
      {label && <p className="text-muted-foreground px-3 pt-2 text-xs">{label}</p>}
      <pre className="overflow-x-auto p-3 pr-12 font-mono text-xs whitespace-pre-wrap break-all">{text}</pre>
      <Button
        size="icon"
        variant="ghost"
        className="absolute top-1.5 right-1.5 size-8"
        aria-label={t("Copy")}
        onClick={() =>
          navigator.clipboard?.writeText(text).then(() => {
            setDone(true);
            setTimeout(() => setDone(false), 1500);
          })
        }
      >
        {done ? <Check /> : <Copy />}
      </Button>
    </div>
  );
}

function PackButton({ pack, label }: { pack: Pack; label: string }) {
  const { t } = useTranslation();
  if (!pack.ready) {
    return (
      <Button disabled variant="outline">
        <Loader2 className="animate-spin" /> {pack.error ? t("Retrying…") : t("Preparing download…")}
      </Button>
    );
  }
  return (
    <Button asChild>
      <a href={pack.url} download>
        <Download /> {label} ({formatBytes(pack.size)})
      </a>
    </Button>
  );
}

const platformLabel: Record<string, string> = {
  "windows-amd64": "Windows",
  "darwin-universal": "macOS",
  "linux-amd64": "Linux x64",
  "linux-arm64": "Linux ARM64",
};

function LauncherCard({ launcher, pageUrl }: { launcher: Launcher; pageUrl: string }) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("Use the launcher")}</CardTitle>
        <CardDescription>
          {t(
            "It keeps your mods in sync with the server and starts the game straight into it. The Steam copy of the game works too.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <ol className="list-decimal space-y-3 pl-5">
          <li className="space-y-2">
            <p>{t("Download it for your system:")}</p>
            <div className="flex flex-wrap gap-2">
              {launcher.downloads.map((d) => (
                <Button key={d.url} asChild variant="outline">
                  <a href={d.url}>
                    <Download /> {platformLabel[`${d.os}-${d.arch}`] ?? `${d.os} ${d.arch}`}
                  </a>
                </Button>
              ))}
            </div>
          </li>
          <li className="space-y-2">
            <p>{t("Open it and add this server with this page's link:")}</p>
            <CopyBlock text={pageUrl} />
          </li>
        </ol>
        <a
          href={launcher.releaseUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="text-muted-foreground inline-flex items-center gap-1 text-xs hover:underline"
        >
          {t("Launcher {{version}}", { version: launcher.version })} <ExternalLink className="size-3" />
        </a>
      </CardContent>
    </Card>
  );
}

const statusStyle = {
  available: "bg-success",
  restarting: "bg-warning animate-pulse",
  unavailable: "bg-destructive",
};

export function ModsPage() {
  const { t } = useTranslation();
  const [data, setData] = useState<PublicData | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!window.location.pathname.endsWith("/")) {
      window.location.replace(`${window.location.pathname}/${window.location.search}`);
      return;
    }
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      try {
        const res = await fetch("data.json", { cache: "no-store" });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const d = (await res.json()) as PublicData;
        if (!stop) {
          setData(d);
          setError("");
        }
      } catch (e) {
        if (!stop) setError((e as Error).message);
      }
      if (!stop) timer = setTimeout(load, 20_000);
    };
    load();
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, []);

  useEffect(() => {
    if (data) document.title = t("{{server}} · mods", { server: data.serverName });
  }, [data, t]);

  if (!data) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        {error ? (
          <p className="text-destructive">{t("This page is unavailable ({{error}}).", { error })}</p>
        ) : (
          <Loader2 className="text-muted-foreground size-6 animate-spin" />
        )}
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-3xl space-y-6 p-4 md:py-10">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-3xl font-semibold tracking-tight">{data.serverName}</h1>
          <p className="text-muted-foreground">{t("Mods required to join this server")}</p>
        </div>
        <div className="flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm" role="status">
          <span className={cn("size-2.5 rounded-full", statusStyle[data.status])} />
          <span className="capitalize">{t(data.status)}</span>
          {data.players != null && (
            <span className="text-muted-foreground">· {t("{{count}} online", { count: data.players })}</span>
          )}
        </div>
      </header>
      <p className="text-muted-foreground -mt-3 text-sm">{t(data.statusMessage)}</p>

      <Tabs defaultValue="join">
        <TabsList>
          <TabsTrigger value="join">{t("Joining our server")}</TabsTrigger>
          <TabsTrigger value="host">{t("Running this mod set yourself")}</TabsTrigger>
        </TabsList>
        <TabsContent value="join" className="space-y-4">
          {data.launcher && (
            <LauncherCard
              launcher={data.launcher}
              // PANEL_PUBLIC_URL wins: this tab may be on an address other players can't reach.
              pageUrl={data.pageUrl ?? window.location.origin + window.location.pathname}
            />
          )}
          <Card>
            <CardHeader>
              <CardTitle>
                {data.launcher ? t("Or install the mods by hand") : t("Install the mods")}
              </CardTitle>
              <CardDescription>
                {t("You only need the files. The server tells your game which mods to load.")}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4 text-sm">
              <ol className="list-decimal space-y-2 pl-5">
                <li>
                  {t("Back up your")} <code className="bg-muted rounded px-1">mods</code>{" "}
                  {t(
                    "folder first. Nothing of yours is deleted, but files with the same name are overwritten.",
                  )}
                </li>
                <li>{t("Download the archive below.")}</li>
                <li>
                  {t("Unzip it into the")} <code className="bg-muted rounded px-1">mods</code>{" "}
                  {t("folder of your Zomboid directory:")}
                  <ul className="text-muted-foreground mt-1 list-disc pl-5">
                    <li>
                      Windows: <code>%USERPROFILE%\Zomboid\mods</code>
                    </li>
                    <li>
                      Linux / macOS: <code>~/Zomboid/mods</code>
                    </li>
                    <li>{t("If you start the game with -cachedir=…, use that folder instead.")}</li>
                  </ul>
                </li>
                <li>{t("Start the game and join the server.")}</li>
              </ol>
              {data.items.length === 0 ? (
                <p className="text-muted-foreground">{t("This server runs without mods.")}</p>
              ) : (
                <div className="space-y-2">
                  <PackButton pack={data.pack} label={t("Download all mods")} />
                  {data.pack.ready && data.pack.sha256 && (
                    <p className="text-muted-foreground font-mono text-xs break-all">
                      SHA-256 {data.pack.sha256}
                    </p>
                  )}
                  <p className="text-muted-foreground text-xs">
                    {t(
                      "Large download? Use the per-item links below; an interrupted download can be resumed.",
                    )}
                  </p>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
        <TabsContent value="host" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("Server configuration")}</CardTitle>
              <CardDescription>
                {t(
                  "Only if you host your own server or a local co-op game with this mod set. Run one command from your Zomboid folder (the one containing mods, Server and Saves).",
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <CopyBlock label="Linux / macOS" text={posixCommand(data)} />
              <CopyBlock label="Windows PowerShell" text={powershellCommand(data)} />
              <p className="text-muted-foreground text-xs">
                {t(
                  "These commands only replace lines that already exist. If a line is missing, add it by hand:",
                )}
              </p>
              <CopyBlock text={`Mods=${data.modsLine}\nWorkshopItems=\nMap=${data.mapLine}`} />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <Card>
        <CardHeader>
          <CardTitle>{t("{{count}} Workshop items", { count: data.items.length })}</CardTitle>
          {data.collection && (
            <CardDescription>
              <a
                href={data.collection.url}
                target="_blank"
                rel="noreferrer noopener"
                className="inline-flex items-center gap-1 hover:underline"
              >
                {t("Steam Workshop collection")} <ExternalLink className="size-3" />
              </a>
            </CardDescription>
          )}
        </CardHeader>
        <CardContent className="divide-y p-0">
          {data.items.map((it) => (
            <details key={it.workshopId} className="group px-6 py-3">
              <summary className="flex cursor-pointer list-none items-center gap-3">
                <ChevronRight className="text-muted-foreground size-4 transition-transform group-open:rotate-90" />
                <span className="min-w-0 flex-1 truncate font-medium">{it.title}</span>
                <span className="text-muted-foreground hidden text-xs sm:inline">{formatBytes(it.size)}</span>
              </summary>
              <div className="mt-3 space-y-2 pl-7 text-sm">
                <p className="text-muted-foreground text-xs">
                  {t("Workshop id")} <span className="font-mono">{it.workshopId}</span> · {t("version of")}{" "}
                  {formatDate(it.timeUpdated)}
                </p>
                <div className="flex flex-wrap gap-1">
                  {it.mods.map((m) => (
                    <Badge key={m.id} variant="secondary" title={`${t("folder")}: ${m.folder}`}>
                      {m.name || m.id}
                    </Badge>
                  ))}
                </div>
                <div className="flex flex-wrap gap-2 pt-1">
                  {it.download.ready ? (
                    <Button asChild size="sm" variant="outline">
                      <a href={it.download.url} download>
                        <Download /> {t("Download")} ({formatBytes(it.download.size)})
                      </a>
                    </Button>
                  ) : (
                    <Button size="sm" variant="outline" disabled>
                      <Loader2 className="animate-spin" /> {t("Preparing…")}
                    </Button>
                  )}
                  <Button asChild size="sm" variant="ghost">
                    <a href={it.url} target="_blank" rel="noreferrer noopener">
                      <ExternalLink /> {t("Workshop page")}
                    </a>
                  </Button>
                </div>
              </div>
            </details>
          ))}
        </CardContent>
      </Card>
      <p className="text-muted-foreground text-center text-xs">
        {t("Updated")} {formatDate(data.generatedAt)}
      </p>
    </div>
  );
}
