// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

import * as React from "react";
import * as AvatarPrimitive from "@radix-ui/react-avatar";
import { cn, initialsFromEmail, stableHue } from "@/lib/utils";

const Avatar = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Root>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Root
    ref={ref}
    className={cn("relative flex h-10 w-10 shrink-0 overflow-hidden rounded-full", className)}
    {...props}
  />
));
Avatar.displayName = AvatarPrimitive.Root.displayName;

const AvatarImage = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Image>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Image>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Image ref={ref} className={cn("aspect-square h-full w-full", className)} {...props} />
));
AvatarImage.displayName = AvatarPrimitive.Image.displayName;

const AvatarFallback = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Fallback>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Fallback>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Fallback
    ref={ref}
    className={cn("flex h-full w-full items-center justify-center rounded-full bg-muted", className)}
    {...props}
  />
));
AvatarFallback.displayName = AvatarPrimitive.Fallback.displayName;

function GeneratedAvatar({ email, className }: { email: string; className?: string }) {
  const hue = stableHue(email);
  return (
    <Avatar className={cn("h-10 w-10 border border-foreground/10", className)}>
      <AvatarFallback
        style={{
          background: `radial-gradient(circle at 30% 25%, hsl(${hue} 55% 80%), hsl(${(hue + 42) % 360} 38% 54%))`,
          color: "hsl(28 24% 14%)",
        }}
        className="text-xs font-semibold"
      >
        {initialsFromEmail(email) || "W3"}
      </AvatarFallback>
    </Avatar>
  );
}

export { Avatar, AvatarImage, AvatarFallback, GeneratedAvatar };
