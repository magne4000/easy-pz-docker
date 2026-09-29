import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center gap-4 py-24 text-center">
      <p className="text-5xl font-semibold">404</p>
      <p className="text-muted-foreground">{t("This page does not exist.")}</p>
      <Button asChild variant="outline">
        <Link to="/">{t("Back to the dashboard")}</Link>
      </Button>
    </div>
  );
}
