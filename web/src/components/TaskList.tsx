import { CheckCircle2, CircleAlert, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { Schemas } from "@/api/client";
import { Progress } from "@/components/ui/progress";
import { formatRelative } from "@/lib/format";

export function TaskList({ tasks, limit = 6 }: { tasks: Schemas["Task"][]; limit?: number }) {
  const { t } = useTranslation();
  if (tasks.length === 0) return <p className="text-muted-foreground text-sm">{t("Nothing has run yet.")}</p>;
  return (
    <ul className="space-y-3">
      {tasks.slice(0, limit).map((task) => (
        <li key={task.id} className="space-y-1">
          <div className="flex items-center gap-2 text-sm">
            {task.state === "running" ? (
              <Loader2 className="text-muted-foreground size-4 animate-spin" />
            ) : task.state === "failed" ? (
              <CircleAlert className="text-destructive size-4" />
            ) : (
              <CheckCircle2 className="text-success size-4" />
            )}
            <span className="font-medium">{task.title}</span>
            <span className="text-muted-foreground ml-auto text-xs">{formatRelative(task.startedAt)}</span>
          </div>
          {task.state === "running" && task.progress >= 0 && (
            <Progress value={task.progress} className="h-1.5" />
          )}
          {(task.error || task.message) && (
            <p
              className={
                task.error ? "text-destructive truncate text-xs" : "text-muted-foreground truncate text-xs"
              }
              title={task.error || task.message}
            >
              {task.error || task.message}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}
