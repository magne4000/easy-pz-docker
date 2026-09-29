import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@/i18n";
import "@/index.css";
import { ThemeProvider } from "@/lib/theme";
import { ModsPage } from "./ModsPage";

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <ThemeProvider>
        <ModsPage />
      </ThemeProvider>
    </StrictMode>,
  );
}
