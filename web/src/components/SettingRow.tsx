import { type ReactNode, useCallback, useState } from "react";

// useEdits tracks unsaved values by key; setting a key back to its original
// value drops the edit.
export function useEdits() {
  const [edits, setEdits] = useState<Record<string, string>>({});
  const set = useCallback((key: string, original: string, value: string) => {
    setEdits((cur) => {
      const next = { ...cur };
      if (value === original) delete next[key];
      else next[key] = value;
      return next;
    });
  }, []);
  const reset = useCallback(() => setEdits({}), []);
  return { edits, set, reset, dirty: Object.keys(edits).length };
}

// SettingRow is one editable option: its name and description on the left,
// the control on the right.
export function SettingRow({
  name,
  badge,
  changed,
  description,
  hint,
  children,
}: {
  name: string;
  badge?: ReactNode;
  changed: boolean;
  description?: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="grid gap-2 p-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] md:gap-6">
      <div className="min-w-0">
        <p className="flex items-center gap-2 font-mono text-sm font-medium">
          {name}
          {badge}
          {changed && <span className="bg-warning size-2 rounded-full" />}
        </p>
        {description && (
          <p className="text-muted-foreground mt-1 text-xs whitespace-pre-line">{description}</p>
        )}
      </div>
      <div className="flex flex-col justify-center gap-1">
        {children}
        {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
      </div>
    </div>
  );
}
