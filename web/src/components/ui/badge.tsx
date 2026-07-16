import type * as React from "react";
import { cn } from "@/lib/utils";

const variants = {
  neutral: "border-border bg-muted text-muted-foreground",
  success: "border-[#b7dfca] bg-[#eef8f2] text-success",
  warning: "border-[#ecd0a6] bg-[#fff4df] text-warning",
  danger: "border-[#efb6ae] bg-[#fff0ee] text-danger",
};

export function Badge({
  className,
  variant = "neutral",
  ...props
}: React.HTMLAttributes<HTMLSpanElement> & { variant?: keyof typeof variants }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium",
        variants[variant],
        className,
      )}
      {...props}
    />
  );
}

