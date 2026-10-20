import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Loader2 } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useSession } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function Login() {
  const session = useSession();
  return session.data?.host === "pelican" ? (
    <PelicanLogin panel={session.data.pelicanPanelUrl} />
  ) : (
    <PasswordLogin />
  );
}

function Frame({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <img src="/favicon.svg" alt="" className="mx-auto mb-2 size-10" />
          <CardTitle>{t("pzman")}</CardTitle>
          <CardDescription>{t("Project Zomboid server manager")}</CardDescription>
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </div>
  );
}

// In Pelican mode, Pelican's login replaces the password: the plugin's
// "Open easy-pz" signs in through /auth/pelican.
function PelicanLogin({ panel }: { panel?: string }) {
  const { t } = useTranslation();
  return (
    <Frame>
      <div className="space-y-4 text-center">
        <p className="font-medium">{t("Open easy-pz from Pelican")}</p>
        <p className="text-muted-foreground text-sm">
          {t("This server is managed in Pelican: open it in the panel and choose Open easy-pz to sign in.")}
        </p>
        {panel && (
          <Button asChild className="w-full">
            <a href={panel}>
              <ExternalLink /> {t("Go to Pelican")}
            </a>
          </Button>
        )}
      </div>
    </Frame>
  );
}

function PasswordLogin() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await unwrap(api.POST("/auth/login", { body: { username, password } }));
      await qc.invalidateQueries({ queryKey: keys.session });
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? t("Invalid username or password.")
          : String((err as Error).message),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <Frame>
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="username">{t("Username")}</Label>
          <Input
            id="username"
            autoComplete="username"
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">{t("Password")}</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        {error && <p className="text-destructive text-sm">{error}</p>}
        <Button type="submit" className="w-full" disabled={busy || !username || !password}>
          {busy && <Loader2 className="animate-spin" />}
          {t("Sign in")}
        </Button>
      </form>
    </Frame>
  );
}
