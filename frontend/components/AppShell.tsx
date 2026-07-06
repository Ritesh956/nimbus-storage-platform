"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import useSWR from "swr";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import {
  FolderIcon,
  SearchIcon,
  TrashIcon,
  PulseIcon,
  ServerIcon,
  ShieldIcon,
  UsersIcon,
  LogoutIcon,
  LogoMark,
} from "./ui/Icons";

const navItems = (orgId: string, platformAdmin: boolean) => [
  { href: `/app/org/${orgId}`, label: "Files", icon: FolderIcon, match: `/app/org/${orgId}/folder` },
  { href: `/app/org/${orgId}/search`, label: "Search", icon: SearchIcon },
  { href: `/app/org/${orgId}/members`, label: "Members", icon: UsersIcon },
  { href: `/app/org/${orgId}/trash`, label: "Trash", icon: TrashIcon },
  { href: `/app/org/${orgId}/activity`, label: "Activity", icon: PulseIcon },
  { href: `/app/org/${orgId}/security`, label: "Security", icon: ShieldIcon },
  // Cluster ops — platform-admin only (the backend 403s everyone else, so
  // don't offer dead navigation). Labeled "Cluster", not "Admin": org
  // governance lives on the Members page, this is infrastructure.
  ...(platformAdmin ? [{ href: `/app/org/${orgId}/admin`, label: "Cluster", icon: ServerIcon }] : []),
];

export function AppShell({ orgId, orgName, children }: { orgId: string; orgName?: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { logout } = useAuth();
  const { data: me } = useSWR("me", () => api.auth.me(), { shouldRetryOnError: false });
  const items = navItems(orgId, me?.is_platform_admin ?? false);
  const isActive = (item: { href: string; match?: string }) =>
    pathname === item.href || (item.match && pathname.startsWith(item.match));

  async function handleLogout() {
    await logout();
    router.replace("/login");
  }

  return (
    <div className="flex min-h-screen flex-col lg:flex-row">
      {/* Desktop sidebar (≥lg) */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-border/50 px-4 py-6 lg:flex">
        <Link href="/app" className="glow-ring mb-8 flex items-center gap-2.5 rounded-lg px-2">
          <LogoMark size={30} />
          <span className="text-[15px] font-semibold tracking-tight">Nimbus</span>
        </Link>

        {orgName && (
          <div className="mb-6 px-2">
            <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-2">
              Organization
            </div>
            <div className="mt-0.5 truncate text-sm text-muted">{orgName}</div>
          </div>
        )}

        <nav className="flex flex-1 flex-col gap-1">
          {items.map((item) => {
            const active = isActive(item);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={`relative flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-colors ${
                  active
                    ? "bg-surface text-foreground"
                    : "text-muted hover:bg-surface/60 hover:text-foreground"
                }`}
              >
                {active && (
                  <span className="gradient-primary absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-full" />
                )}
                <Icon size={16} className={active ? "text-accent" : "text-muted-2"} />
                {item.label}
              </Link>
            );
          })}
        </nav>

        <button
          onClick={handleLogout}
          className="glow-ring mt-4 flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2 text-[13px] font-medium text-muted transition-colors hover:border-border-strong hover:text-foreground"
        >
          <LogoutIcon size={16} className="text-muted-2" />
          Log out
        </button>
      </aside>

      {/* Mobile top bar (<lg) */}
      <header className="sticky top-0 z-20 flex items-center justify-between border-b border-border/50 bg-background/90 px-4 py-3 backdrop-blur lg:hidden">
        <Link href="/app" className="glow-ring flex items-center gap-2 rounded-lg">
          <LogoMark size={26} />
          <span className="text-[15px] font-semibold tracking-tight">Nimbus</span>
        </Link>
        <div className="flex items-center gap-2">
          {orgName && <span className="max-w-36 truncate text-xs text-muted">{orgName}</span>}
          <button
            onClick={handleLogout}
            title="Log out"
            className="glow-ring rounded-lg p-2 text-muted-2 transition-colors hover:text-foreground"
          >
            <LogoutIcon size={16} />
          </button>
        </div>
      </header>

      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-4 pb-24 pt-6 sm:px-6 lg:px-8 lg:py-8">{children}</div>
      </main>

      {/* Mobile bottom tab bar (<lg) */}
      <nav className={`fixed inset-x-0 bottom-0 z-20 grid ${items.length === 7 ? "grid-cols-7" : "grid-cols-6"} border-t border-border/60 bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden`}>
        {items.map((item) => {
          const active = isActive(item);
          const Icon = item.icon;
          return (
            <Link key={item.href} href={item.href} className="relative flex flex-col items-center gap-1 py-2.5">
              {active && (
                <span className="gradient-primary absolute left-1/2 top-0 h-[2.5px] w-8 -translate-x-1/2 rounded-full" />
              )}
              <Icon size={18} className={active ? "text-accent" : "text-muted-2"} />
              <span className={`text-[10px] font-medium ${active ? "text-foreground" : "text-muted-2"}`}>
                {item.label}
              </span>
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
