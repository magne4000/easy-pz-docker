import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router-dom";
import { onUnauthorized } from "@/api/client";
import { keys } from "@/api/keys";
import { useSession } from "@/api/queries";
import { Layout } from "@/components/Layout";
import { type ConnectionState, connectEvents } from "@/events";
import { Login } from "@/pages/Login";

const Dashboard = lazy(() => import("@/pages/Dashboard"));
const Console = lazy(() => import("@/pages/Console"));
const Mods = lazy(() => import("@/pages/Mods"));
const Backups = lazy(() => import("@/pages/Backups"));
const Schedules = lazy(() => import("@/pages/Schedules"));
const ServerConfig = lazy(() => import("@/pages/ServerConfig"));
const Sandbox = lazy(() => import("@/pages/Sandbox"));
const SettingsPage = lazy(() => import("@/pages/Settings"));
const NotFound = lazy(() => import("@/pages/NotFound"));

function Spinner() {
  return (
    <div className="flex min-h-40 items-center justify-center">
      <Loader2 className="text-muted-foreground size-6 animate-spin" />
    </div>
  );
}

function WaitingForBackend() {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-3 p-4 text-center">
      <Loader2 className="text-muted-foreground size-6 animate-spin" />
      <p className="font-medium">{t("Waiting for the pzman backend…")}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {t("It is starting or restarting. This page continues on its own as soon as it answers.")}
      </p>
    </div>
  );
}

function Authenticated() {
  const qc = useQueryClient();
  const [connection, setConnection] = useState<ConnectionState>("connecting");
  useEffect(() => connectEvents(qc, setConnection), [qc]);
  return (
    <Layout connection={connection}>
      <Suspense fallback={<Spinner />}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/console" element={<Console />} />
          <Route path="/mods" element={<Mods />} />
          <Route path="/backups" element={<Backups />} />
          <Route path="/schedules" element={<Schedules />} />
          <Route path="/config" element={<ServerConfig />} />
          <Route path="/sandbox" element={<Sandbox />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </Suspense>
    </Layout>
  );
}

export function App() {
  const qc = useQueryClient();
  const session = useSession();
  useEffect(() => onUnauthorized(() => qc.invalidateQueries({ queryKey: keys.session })), [qc]);
  if (session.isPending) return session.failureCount > 0 ? <WaitingForBackend /> : <Spinner />;
  if (!session.data?.authenticated) return <Login />;
  return <Authenticated />;
}
