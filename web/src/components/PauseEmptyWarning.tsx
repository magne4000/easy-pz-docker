import { AlertTriangle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useBackups } from "@/api/queries";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

// PauseEmptyWarning explains why no backup is ever skipped: with PauseEmpty
// off the game clock runs on an empty server, so the world always changed.
export function PauseEmptyWarning({ className }: { className?: string }) {
  const { t } = useTranslation();
  const q = useBackups();
  if (q.data?.pauseEmpty !== false) return null;
  return (
    <Alert className={className}>
      <AlertTriangle />
      <AlertTitle>{t("Backups are never skipped")}</AlertTitle>
      <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
        <span>
          {t(
            "PauseEmpty is off in the server config: the game clock keeps running with nobody online, so the world changes between every backup and each one is taken. Turn PauseEmpty on to skip backups when nobody played.",
          )}
        </span>
        <Button size="sm" variant="outline" asChild>
          <Link to="/config">{t("Server config")}</Link>
        </Button>
      </AlertDescription>
    </Alert>
  );
}
