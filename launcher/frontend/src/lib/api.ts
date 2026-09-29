import { CancelError } from "@wailsio/runtime";

export * as App from "@bindings/launcher/appservice";
export * as Config from "@bindings/launcher/internal/config/models";
export * as Core from "@bindings/launcher/internal/core/models";
export * as Launcher from "@bindings/launcher/internal/core/service";
export * as Saved from "@bindings/launcher/internal/serverlist/models";

export function errorMessage(err: unknown): string {
  if (err && typeof err === "object" && "message" in err) {
    return String((err as { message: unknown }).message);
  }
  return String(err);
}

// core.ErrForeignMods: "foreign-mods: ModA, ModB".
const foreignPrefix = "foreign-mods: ";

export function foreignMods(err: unknown): string[] | null {
  const msg = errorMessage(err);
  const i = msg.indexOf(foreignPrefix);
  return i < 0 ? null : msg.slice(i + foreignPrefix.length).split(", ");
}

export function isCancelled(err: unknown): boolean {
  return err instanceof CancelError || /context canceled/.test(errorMessage(err));
}
