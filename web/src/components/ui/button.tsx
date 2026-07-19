import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex h-11 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-lg border px-4 text-sm font-semibold transition-[color,background-color,border-color,box-shadow,transform] duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 active:translate-y-px disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-45",
  {
    variants: {
      variant: {
        default: "border-primary bg-primary text-primary-foreground shadow-[0_7px_18px_rgba(11,107,123,0.18)] hover:border-[#085d6b] hover:bg-[#085d6b] hover:shadow-[0_9px_24px_rgba(11,107,123,0.24)]",
        secondary: "border-border bg-card text-foreground shadow-[0_1px_2px_rgba(18,34,53,0.04)] hover:border-[#b7c8c5] hover:bg-muted/65",
        outline: "border-border bg-transparent text-foreground hover:border-primary/45 hover:bg-primary/5 hover:text-primary",
        ghost: "border-transparent bg-transparent hover:bg-muted/75",
        danger: "border-danger bg-danger text-white shadow-[0_7px_18px_rgba(185,56,47,0.15)] hover:bg-[#9f2f28]",
      },
      size: {
        default: "h-11 px-4",
        sm: "h-11 px-3 text-xs",
        icon: "h-11 w-11 px-0",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return <Comp className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}
