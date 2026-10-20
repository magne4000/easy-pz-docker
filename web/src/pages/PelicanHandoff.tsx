import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import { api, unwrap } from "@/api/client";
import { keys } from "@/api/keys";
import { useSession } from "@/api/queries";
import { setSessionToken } from "@/api/session";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { handoffOnce } from "@/lib/handoff";

// PelicanHandoff signs in with the token Pelican's "Open easy-pz" put in the
// URL fragment (/auth/pelican#t=<token>[&to=<path>]), keeps the session token
// it gets back (no cookie on Pelican), then continues to `to` or the dashboard.
export function PelicanHandoff() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const session = useSession();
  const [error, setError] = useState("");
  // A new #t= in this tab (same-document navigation) is a new sign-in.
  const [visit, setVisit] = useState(0);
  useEffect(() => {
    const onHash = () => setVisit((n) => n + 1);
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  // biome-ignore lint/correctness/useExhaustiveDependencies: visit re-reads the fragment
  useEffect(() => {
    let current = true;
    const signIn = handoffOnce(async (token) => {
      const { token: session, ...s } = await unwrap(api.POST("/auth/pelican", { body: { token } }));
      setSessionToken(session ?? null);
      return s;
    });
    if (!signIn) {
      setError(t("No sign-in token: open easy-pz again from the Pelican panel."));
      return;
    }
    setError("");
    signIn.result.then(
      (s) => {
        if (!current) return;
        qc.setQueryData(keys.session, s);
        qc.invalidateQueries();
        navigate(signIn.to ?? "/", { replace: true });
      },
      (err: Error) => current && setError(err.message),
    );
    return () => {
      current = false;
    };
  }, [qc, navigate, t, visit]);

  const panel = session.data?.pelicanPanelUrl;
  return (
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <img src="/favicon.svg" alt="" className="mx-auto mb-2 size-10" />
          <CardTitle>{t("pzman")}</CardTitle>
          <CardDescription>{error ? t("Sign-in failed") : t("Signing in with Pelican…")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {error ? (
            <>
              <p className="text-destructive text-center text-sm">{error}</p>
              {panel && (
                <Button asChild className="w-full">
                  <a href={panel}>
                    <ExternalLink /> {t("Back to Pelican")}
                  </a>
                </Button>
              )}
              {session.data?.authenticated && (
                <Button asChild variant="outline" className="w-full">
                  <Link to="/" replace>
                    {t("Continue to the dashboard")}
                  </Link>
                </Button>
              )}
            </>
          ) : (
            <div className="flex justify-center">
              <Loader2 className="text-muted-foreground size-6 animate-spin" />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
