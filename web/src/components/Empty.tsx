import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export function Empty({
  icon: Icon,
  title,
  children,
}: {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-10 text-center">
      <Icon className="text-muted-foreground size-8" />
      <p className="font-medium">{title}</p>
      {children && <div className="text-muted-foreground max-w-md text-sm">{children}</div>}
    </div>
  );
}
