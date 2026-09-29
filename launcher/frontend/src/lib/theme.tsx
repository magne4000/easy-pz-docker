import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ReactNode } from "react";

export { useTheme } from "next-themes";

export function ThemeProvider({ children }: { children: ReactNode }) {
  return (
    <NextThemesProvider attribute="class" storageKey="easypz.theme" defaultTheme="system" enableSystem>
      {children}
    </NextThemesProvider>
  );
}
