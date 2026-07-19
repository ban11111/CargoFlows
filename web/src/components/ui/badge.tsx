import type * as React from "react";
import { cn } from "@/lib/utils";

const variants = {
  neutral: "border-border bg-muted/70 text-[#4d606d] before:hidden",
  success: "border-[#b7dfca] bg-[#eef8f2] text-success before:bg-success",
  warning: "border-[#ecd0a6] bg-[#fff4df] text-warning before:bg-warning",
  danger: "border-[#efb6ae] bg-[#fff0ee] text-danger before:bg-danger",
};

export function Badge({
  className,
  variant = "neutral",
  ...props
}: React.HTMLAttributes<HTMLSpanElement> & { variant?: keyof typeof variants }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold before:h-1.5 before:w-1.5 before:rounded-full",
        variants[variant],
        className,
      )}
      {...props}
    />
  );
}
