"use client";

import { useRouter } from "next/navigation";
import { LogOutIcon, MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import type { User } from "@/lib/api";
import { useLogout } from "@/lib/queries";
import { initials } from "@/lib/format";

const AVATAR_TONES = [
  "from-[#2563eb] to-[#0c5cab]",
  "from-[#0891b2] to-[#0e7490]",
  "from-[#7c3aed] to-[#5b21b6]",
  "from-[#db2777] to-[#9d174d]",
  "from-[#d97706] to-[#b45309]",
  "from-[#059669] to-[#047857]",
];

function avatarTone(name: string) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return AVATAR_TONES[h % AVATAR_TONES.length];
}

export function UserAvatar({ user, className }: { user: Pick<User, "name">; className?: string }) {
  return (
    <Avatar className={className}>
      <AvatarFallback
        className={`bg-linear-to-br text-xs font-semibold text-white shadow-[inset_0_1px_0_rgb(255_255_255/0.2)] ${avatarTone(user.name)}`}
      >
        {initials(user.name)}
      </AvatarFallback>
    </Avatar>
  );
}

/** Shared dropdown body for the account menu (sidebar footer + org picker header). */
export function UserMenuItems({ user }: { user: User }) {
  const router = useRouter();
  const { theme, setTheme } = useTheme();
  const logout = useLogout();

  async function signOut() {
    try {
      await logout.mutateAsync();
    } catch {
      toast.error("Couldn't sign out cleanly; clearing local session.");
    }
    router.replace("/login");
    router.refresh();
  }

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel className="flex items-center gap-2 py-1.5 font-normal">
          <UserAvatar user={user} className="size-8" />
          <div className="grid min-w-0 text-left text-sm leading-tight">
            <span className="truncate font-medium text-foreground">{user.name}</span>
            <span className="truncate text-xs text-muted-foreground">{user.email}</span>
          </div>
        </DropdownMenuLabel>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuLabel>Theme</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme ?? "system"} onValueChange={(v) => setTheme(String(v))}>
          <DropdownMenuRadioItem value="light">
            <SunIcon /> Light
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <MoonIcon /> Dark
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <MonitorIcon /> System
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem onClick={signOut}>
        <LogOutIcon /> Sign out
      </DropdownMenuItem>
    </>
  );
}
