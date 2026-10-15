import { Clipboard } from "@wailsio/runtime";
import { CheckCircle2Icon, CheckIcon, CopyIcon, Loader2Icon, PlayIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { toast } from "sonner";
import { useLaunch } from "@/components/LaunchProvider";
import { Button } from "@/components/ui/button";
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { type Config, errorMessage, type Saved } from "@/lib/api";

// The game's own labels (Multiplayer menu), which the steps quote verbatim.
const gameUI = {
  multiplayer: "Multiplayer",
  addServer: "Add Server",
  serverName: "Server name",
  address: "IP/Domain",
  port: "Port",
  serverPassword: "Server Password",
  addAccount: "Add New Account",
  username: "Username",
  password: "Password",
  rememberPassword: "Remember password",
};

const label = <b className="font-medium text-foreground" />;

// Accounts the launcher can join with: "Remember password" ticked, password authentication.
export function autoLogins(saved?: Saved.Server | null): Saved.Account[] {
  return (saved?.accounts ?? []).filter((a) => a.autoLogin);
}

// Dialog content guiding the player through adding an account in the game, which is what lets the launcher join.
// `saved` is the game's entry at the server's address, kept fresh by the caller.
export function JoinSetup({
  server,
  saved,
  onClose,
}: {
  server: Config.Server;
  saved?: Saved.Server | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const launch = useLaunch();
  const hasAddress = !!server.host && !!server.port;
  const ready = autoLogins(saved).length > 0;
  const secondFactor = !!saved?.accounts.some((a) => a.authType !== 1);

  const steps: { key: string; body: ReactNode }[] = [
    {
      key: "start",
      body: (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>{t("Start the game.")}</span>
          <Button
            size="xs"
            variant="outline"
            onClick={() => launch.run("start", server)}
            disabled={launch.busy}
          >
            <PlayIcon />
            {t("Start")}
          </Button>
        </div>
      ),
    },
  ];
  if (!saved) {
    steps.push({
      key: "server",
      body: (
        <>
          <p>
            {hasAddress ? (
              <Trans
                i18nKey="In <b>{{menu}}</b>, choose <b>{{add}}</b> and fill in:"
                values={{ menu: gameUI.multiplayer, add: gameUI.addServer }}
                components={{ b: label }}
              />
            ) : (
              <Trans
                i18nKey="In <b>{{menu}}</b>, choose <b>{{add}}</b> and fill in <b>{{name}}</b>, <b>{{address}}</b> and <b>{{port}}</b>: ask the server admin for them."
                values={{
                  menu: gameUI.multiplayer,
                  add: gameUI.addServer,
                  name: gameUI.serverName,
                  address: gameUI.address,
                  port: gameUI.port,
                }}
                components={{ b: label }}
              />
            )}
          </p>
          {hasAddress && (
            <dl className="grid grid-cols-[auto_1fr_auto] items-center gap-x-3 gap-y-1 rounded-md border px-3 py-2">
              <Field name={gameUI.serverName} value={server.name} />
              <Field name={gameUI.address} value={server.host} />
              <Field name={gameUI.port} value={String(server.port)} />
            </dl>
          )}
          <p className="text-muted-foreground text-xs">
            <Trans
              i18nKey="<b>{{field}}</b>: only if the server has one."
              values={{ field: gameUI.serverPassword }}
              components={{ b: label }}
            />
          </p>
        </>
      ),
    });
  }
  // With no address the player names the server in the game; otherwise it is the name copied above.
  const gameName = saved?.name ?? (hasAddress ? server.name : "");
  const account = {
    menu: gameUI.multiplayer,
    name: gameName,
    add: gameUI.addAccount,
    username: gameUI.username,
    password: gameUI.password,
    remember: gameUI.rememberPassword,
  };
  steps.push({
    key: "account",
    body: (
      <>
        <p>
          {gameName ? (
            <Trans
              i18nKey="In <b>{{menu}}</b>, select <b>{{name}}</b>, then <b>{{add}}</b>: enter your <b>{{username}}</b> and <b>{{password}}</b> on this server and tick <b>{{remember}}</b>."
              values={account}
              components={{ b: label }}
            />
          ) : (
            <Trans
              i18nKey="In <b>{{menu}}</b>, select the server, then <b>{{add}}</b>: enter your <b>{{username}}</b> and <b>{{password}}</b> on this server and tick <b>{{remember}}</b>."
              values={account}
              components={{ b: label }}
            />
          )}
        </p>
        {secondFactor && (
          <p className="text-muted-foreground text-xs">
            {t("Accounts using another authentication type than Password cannot be used by the launcher.")}
          </p>
        )}
      </>
    ),
  });
  steps.push({ key: "join", body: <p>{t("Join the server once with this account.")}</p> });
  if (!hasAddress) {
    steps.push({
      key: "address",
      body: (
        <p>
          <Trans
            i18nKey="Back here, enter the same address and port in <b>⋮ › Edit</b>."
            components={{ b: label }}
          />
        </p>
      ),
    });
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("Set up auto-join for {{name}}", { name: server.name })}</DialogTitle>
        <DialogDescription>
          {t(
            "Add your account in the game once, with its password remembered: the launcher then joins the server for you.",
          )}
        </DialogDescription>
      </DialogHeader>
      {ready ? (
        <p className="flex items-center gap-2 text-sm text-success">
          <CheckCircle2Icon className="size-4 shrink-0" />
          {t("Account found: Join now connects automatically.")}
        </p>
      ) : (
        <>
          <ol className="space-y-3 text-sm">
            {steps.map((s, i) => (
              <li key={s.key} className="flex gap-3">
                <span className="flex size-5 shrink-0 items-center justify-center rounded-full border text-muted-foreground text-xs tabular-nums">
                  {i + 1}
                </span>
                <div className="min-w-0 flex-1 space-y-2 pt-px">{s.body}</div>
              </li>
            ))}
          </ol>
          {hasAddress && (
            <p className="flex items-center gap-2 text-muted-foreground text-sm">
              <Loader2Icon className="size-4 shrink-0 animate-spin" />
              {t("Waiting for the game to remember your password…")}
            </p>
          )}
        </>
      )}
      <DialogFooter>
        <Button variant={ready ? "default" : "outline"} onClick={onClose}>
          {ready ? t("Done") : t("Later")}
        </Button>
      </DialogFooter>
    </>
  );
}

function Field({ name, value }: { name: string; value: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await Clipboard.SetText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };
  return (
    <>
      <dt className="text-muted-foreground text-xs">{name}</dt>
      <dd className="truncate font-mono text-xs">{value}</dd>
      <Button variant="ghost" size="icon-xs" onClick={copy} aria-label={t("Copy {{name}}", { name })}>
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    </>
  );
}
