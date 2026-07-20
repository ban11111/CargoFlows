import { AlertTriangle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";

export function ErrorNotice({
  title,
  message,
  recovery,
  requestId,
  actionLabel,
  onAction,
  className = "",
}: {
  title: string;
  message: string;
  recovery?: string;
  requestId?: string;
  actionLabel?: string;
  onAction?: () => void;
  className?: string;
}) {
  return (
    <div
      aria-live="assertive"
      className={`rounded-lg border border-danger/30 bg-danger/5 p-4 ${className}`}
      role="alert"
    >
      <div className="flex items-start gap-3">
        <AlertTriangle aria-hidden="true" className="mt-0.5 h-5 w-5 shrink-0 text-danger" />
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-danger">{title}</p>
          <p className="mt-1 break-words text-sm leading-6 text-foreground">{message}</p>
          {recovery ? <p className="mt-2 text-sm leading-6 text-muted-foreground">{recovery}</p> : null}
          {requestId ? <p className="mt-2 font-mono text-xs text-muted-foreground">ID: {requestId}</p> : null}
          {actionLabel && onAction ? (
            <Button className="mt-3 min-h-11" onClick={onAction} type="button" variant="secondary">
              <RefreshCw className="h-4 w-4" />{actionLabel}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
