import type * as React from "react";
import { cn } from "@/lib/utils";

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        "min-h-28 w-full rounded-lg border border-border bg-card px-3.5 py-3 text-sm text-foreground shadow-[inset_0_1px_2px_rgba(18,34,53,0.025)] outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground/75 hover:border-[#b7c8c5] focus:border-primary focus:ring-3 focus:ring-primary/10 disabled:cursor-not-allowed disabled:bg-muted/60",
        className,
      )}
      {...props}
    />
  );
}
