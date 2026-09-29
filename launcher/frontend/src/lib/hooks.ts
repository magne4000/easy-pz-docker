import { useCallback, useEffect, useRef, useState } from "react";
import { type Config, type Core, errorMessage, Launcher } from "@/lib/api";

const pollMs = 15_000;

export function useStatus(server: Config.Server, paused: boolean, refreshKey: number) {
  const [status, setStatus] = useState<Core.Status | null>(null);
  const [error, setError] = useState<string | null>(null);
  const serverRef = useRef(server);
  serverRef.current = server;

  const refresh = useCallback(async () => {
    try {
      setStatus(await Launcher.Status(serverRef.current.id));
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  const key = `${server.id}|${server.host}|${server.port}|${server.pageUrl ?? ""}|${refreshKey}`;
  // biome-ignore lint/correctness/useExhaustiveDependencies: key captures the server fields that matter
  useEffect(() => {
    refresh();
  }, [key, refresh]);

  useEffect(() => {
    if (paused) return;
    const t = setInterval(refresh, pollMs);
    return () => clearInterval(t);
  }, [paused, refresh]);

  return { status, error, refresh };
}
