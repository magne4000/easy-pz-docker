import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ReactNode } from "react";

export { useTheme } from "next-themes";

// The key predates next-themes; keeping it preserves saved choices.
export function ThemeProvider({ children }: { children: ReactNode }) {
  return (
    <NextThemesProvider attribute="class" storageKey="pzman.theme" defaultTheme="system" enableSystem>
      {children}
    </NextThemesProvider>
  );
}
