import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const styles: Record<string, string> = {
  running: "bg-success/15 text-success border-success/30",
  starting: "bg-warning/15 text-warning border-warning/30",
  stopping: "bg-warning/15 text-warning border-warning/30",
  stopped: "bg-muted text-muted-foreground",
  crashed: "bg-destructive/15 text-destructive border-destructive/30",
};

export function StatusBadge({ state, className }: { state?: string; className?: string }) {
  const { t } = useTranslation();
  const s = state ?? "unknown";
  return (
    <Badge variant="outline" className={cn("gap-1.5 capitalize", styles[s], className)}>
      <span
        className={cn(
          "size-1.5 rounded-full bg-current",
          (s === "starting" || s === "stopping") && "animate-pulse",
        )}
      />
      {t(s)}
    </Badge>
  );
}
