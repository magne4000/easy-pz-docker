import type { Schemas } from "@/api/client";

// modsPageHref is where the player mod page opens from the admin UI. Behind
// Pelican the page is served through the panel's proxy under PANEL_PUBLIC_URL
// (its asset and download links carry that prefix), never on pzman's own port;
// in Docker mode it is this origin's path.
export function modsPageHref(
  sys: Pick<Schemas["SystemOutputBody"], "host" | "publicUrl" | "modsPagePath">,
): string {
  if (!sys.modsPagePath) return "";
  return sys.host === "pelican" ? `${sys.publicUrl ?? ""}${sys.modsPagePath}` : sys.modsPagePath;
}
