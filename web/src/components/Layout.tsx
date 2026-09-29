import { useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  Biohazard,
  CalendarClock,
  FileCog,
  LayoutDashboard,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Package,
  Settings,
  Sun,
  Terminal,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { NavLink } from "react-router-dom";
import { api } from "@/api/client";
import { keys } from "@/api/keys";
import { useServerStatus, useSystem } from "@/api/queries";
import { StatusBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ConnectionState } from "@/events";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const nav = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/console", label: "Console", icon: Terminal },
  { to: "/mods", label: "Mods", icon: Package },
  { to: "/backups", label: "Backups", icon: Archive },
  { to: "/schedules", label: "Scheduler", icon: CalendarClock },
  { to: "/config", label: "Server config", icon: FileCog },
  { to: "/sandbox", label: "Sandbox", icon: Biohazard },
  { to: "/settings", label: "Settings", icon: Settings },
];

function ThemeToggle() {
  const { t } = useTranslation();
  const { theme, setTheme } = useTheme();
  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("Theme")}>
          <Icon className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => setTheme("light")}>
          <Sun /> {t("Light")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("dark")}>
          <Moon /> {t("Dark")}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("system")}>
          <Monitor /> {t("System")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function Connection({ state }: { state: ConnectionState }) {
  const { t } = useTranslation();
  const label = state === "open" ? t("Live") : state === "connecting" ? t("Reconnecting…") : t("Offline");
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
          <span
            className={cn(
              "size-2 rounded-full",
              state === "open"
                ? "bg-success"
                : state === "connecting"
                  ? "bg-warning animate-pulse"
                  : "bg-destructive",
            )}
          />
          <span className="hidden sm:inline">{label}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{t("Live updates from the server")}</TooltipContent>
    </Tooltip>
  );
}

export function Layout({ children, connection }: { children: ReactNode; connection: ConnectionState }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const status = useServerStatus();
  const system = useSystem();
  const [open, setOpen] = useState(false);

  const logout = async () => {
    await api.POST("/auth/logout");
    qc.clear();
    qc.invalidateQueries({ queryKey: keys.session });
  };

  const links = (
    <nav className="flex flex-col gap-1">
      {nav.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          end={to === "/"}
          onClick={() => setOpen(false)}
          className={({ isActive }) =>
            cn(
              "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              isActive
                ? "bg-sidebar-accent text-foreground"
                : "text-muted-foreground hover:bg-sidebar-accent/60",
            )
          }
        >
          <Icon className="size-4" />
          {t(label)}
        </NavLink>
      ))}
    </nav>
  );

  return (
    <div className="flex min-h-svh">
      <aside className="bg-sidebar border-sidebar-border hidden w-60 shrink-0 flex-col border-r p-4 md:flex">
        <Brand name={system.data?.serverName} />
        <div className="mt-6">{links}</div>
        <p className="text-muted-foreground mt-auto px-3 text-xs">pzman {system.data?.version}</p>
      </aside>
      {open && (
        <div className="fixed inset-0 z-40 md:hidden">
          <button
            type="button"
            aria-label={t("Close menu")}
            className="absolute inset-0 bg-black/40"
            onClick={() => setOpen(false)}
          />
          <aside className="bg-sidebar absolute inset-y-0 left-0 w-64 p-4 shadow-xl">
            <Brand name={system.data?.serverName} />
            <div className="mt-6">{links}</div>
          </aside>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="bg-background/80 sticky top-0 z-30 flex h-14 items-center gap-3 border-b px-4 backdrop-blur">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setOpen(true)}
            aria-label={t("Menu")}
          >
            <Menu className="size-5" />
          </Button>
          <StatusBadge state={status.data?.state} />
          {status.data?.operation && (
            <span className="text-muted-foreground truncate text-xs">
              {t("busy")}: {t(status.data.operation)}
            </span>
          )}
          {system.data?.drivers === "fake" && (
            <span className="bg-warning/15 text-warning rounded px-2 py-0.5 text-xs font-medium">
              {t("fake drivers")}: {system.data.scenario}
            </span>
          )}
          <div className="ml-auto flex items-center gap-2">
            <Connection state={connection} />
            <ThemeToggle />
            <Button variant="ghost" size="icon" onClick={logout} aria-label={t("Log out")}>
              <LogOut className="size-4" />
            </Button>
          </div>
        </header>
        <main className="mx-auto w-full max-w-6xl flex-1 p-4 md:p-8">{children}</main>
      </div>
    </div>
  );
}

function Brand({ name }: { name?: string }) {
  return (
    <div className="flex items-center gap-2 px-3">
      <img src="/favicon.svg" alt="" className="size-7" />
      <div className="min-w-0">
        <p className="text-sm leading-tight font-semibold">pzman</p>
        <p className="text-muted-foreground truncate text-xs">{name ?? "…"}</p>
      </div>
    </div>
  );
}
