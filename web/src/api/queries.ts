import { type QueryKey, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, unwrap } from "./client";
import { keys } from "./keys";

// The session check gates the whole UI: while the backend is unreachable
// (starting, or restarting under `make dev`), keep polling every second
// instead of backing off, so the page appears as soon as it answers.
export const useSession = () =>
  useQuery({
    queryKey: keys.session,
    queryFn: () => unwrap(api.GET("/auth/session")),
    staleTime: 60_000,
    retry: true,
    retryDelay: 1000,
  });

export const useServerStatus = () =>
  useQuery({
    queryKey: keys.server,
    queryFn: () => unwrap(api.GET("/server/status")),
    refetchInterval: 15_000,
  });

export const useConsole = () =>
  useQuery({
    queryKey: keys.console,
    queryFn: () => unwrap(api.GET("/console/history", { params: { query: { limit: 2000 } } })),
    staleTime: Number.POSITIVE_INFINITY,
  });

export const useTasks = () => useQuery({ queryKey: keys.tasks, queryFn: () => unwrap(api.GET("/tasks")) });
export const useSystem = () =>
  useQuery({ queryKey: keys.system, queryFn: () => unwrap(api.GET("/system")), refetchInterval: 60_000 });
export const useUpdates = () =>
  useQuery({ queryKey: keys.updates, queryFn: () => unwrap(api.GET("/updates")) });
export const useMods = () => useQuery({ queryKey: keys.mods, queryFn: () => unwrap(api.GET("/mods")) });
export const useConflicts = (enabled: boolean) =>
  useQuery({ queryKey: keys.conflicts, queryFn: () => unwrap(api.GET("/mods/conflicts")), enabled });
export const useBackups = () =>
  useQuery({ queryKey: keys.backups, queryFn: () => unwrap(api.GET("/backups")) });
export const useBackup = (id: number | null) =>
  useQuery({
    queryKey: [...keys.backups, id],
    queryFn: () => unwrap(api.GET("/backups/{id}", { params: { path: { id: id ?? 0 } } })),
    enabled: id != null,
  });
export const useSchedules = () =>
  useQuery({ queryKey: keys.schedules, queryFn: () => unwrap(api.GET("/schedules")) });
export const useIni = () =>
  useQuery({ queryKey: [...keys.config, "ini"], queryFn: () => unwrap(api.GET("/config/ini")) });
export const useSandbox = () =>
  useQuery({ queryKey: [...keys.config, "sandbox"], queryFn: () => unwrap(api.GET("/config/sandbox")) });
export const usePaths = () =>
  useQuery({ queryKey: [...keys.config, "paths"], queryFn: () => unwrap(api.GET("/config/paths")) });
export const useSettings = () =>
  useQuery({ queryKey: keys.settings, queryFn: () => unwrap(api.GET("/settings")) });

// useAction wraps a mutation with toasts and invalidation of the given keys.
export function useAction<V = void, R = unknown>(
  fn: (v: V) => Promise<R>,
  opts: { success?: string | ((r: R) => string); invalidate?: QueryKey[] } = {},
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (r) => {
      const msg = typeof opts.success === "function" ? opts.success(r) : opts.success;
      if (msg) toast.success(msg);
      for (const k of opts.invalidate ?? []) qc.invalidateQueries({ queryKey: k });
    },
    onError: (e: Error) => toast.error(e.message),
  });
}
