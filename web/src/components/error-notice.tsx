import { AlertTriangle, LoaderCircle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";

export function ErrorNotice({
  title,
  message,
  stage,
  recovery,
  requestId,
  code,
  actionLabel,
  onAction,
  actionPending = false,
  className = "",
  labels,
}: {
  title: string;
  message: string;
  stage?: string;
  recovery?: string;
  requestId?: string;
  code?: string;
  actionLabel?: string;
  onAction?: () => void;
  actionPending?: boolean;
  className?: string;
  labels?: { stage: string; reason: string; recovery: string; diagnostics: string };
}) {
	const copy = labels ?? { stage: "Failed at", reason: "What happened", recovery: "What to do", diagnostics: "Technical details" };
  return (
    <div
      aria-live="assertive"
      className={`rounded-lg border border-danger/30 bg-danger/5 p-3 ${className}`}
      role="alert"
    >
      <div className="flex items-start gap-2.5">
        <AlertTriangle aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1"><p className="text-sm font-semibold text-danger">{title}</p>{stage ? <span className="text-xs text-muted-foreground">{copy.stage}：<span className="font-medium text-foreground">{stage}</span></span> : null}</div>
          <p className="mt-1 break-words text-sm leading-5 text-foreground"><span className="sr-only">{copy.reason}：</span>{message}</p>
          {recovery ? <p className="mt-1 text-xs leading-5 text-muted-foreground"><span className="font-medium">{copy.recovery}：</span>{recovery}</p> : null}
          <div className="mt-2 flex flex-wrap items-center gap-2">
          {code || requestId ? <details className="text-xs"><summary className="cursor-pointer font-medium text-muted-foreground">{copy.diagnostics}</summary><div className="mt-1 space-y-1 font-mono text-muted-foreground">{code ? <p>Code: {code}</p> : null}{requestId ? <p className="break-all">ID: {requestId}</p> : null}</div></details> : null}
          {actionLabel && onAction ? (
            <Button aria-busy={actionPending} className="min-h-11" disabled={actionPending} onClick={onAction} size="sm" type="button" variant="secondary">
              {actionPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <RefreshCw className="h-4 w-4" />}{actionLabel}
            </Button>
          ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
