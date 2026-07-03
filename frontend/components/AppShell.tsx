"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import {
  FolderIcon,
  SearchIcon,
  TrashIcon,
  PulseIcon,
  ServerIcon,
  LogoutIcon,
  LogoMark,
} from "./ui/Icons";

const navItems = (orgId: string) => [
  { href: `/app/org/${orgId}`, label: "Files", icon: FolderIcon, match: `/app/org/${orgId}/folder` },
  { href: `/app/org/${orgId}/search`, label: "Search", icon: SearchIcon },
  { href: `/app/org/${orgId}/trash`, label: "Trash", icon: TrashIcon },
  { href: `/app/org/${orgId}/activity`, label: "Activity", icon: PulseIcon },
  { href: `/app/org/${orgId}/admin`, label: "Admin", icon: ServerIcon },
];

export function AppShell({ orgId, orgName, children }: { orgId: string; orgName?: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { logout } = useAuth();

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-60 shrink-0 flex-col border-r border-border/50 px-4 py-6">
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
          {navItems(orgId).map((item) => {
            const active = pathname === item.href || (item.match && pathname.startsWith(item.match));
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
          onClick={async () => {
            await logout();
            router.replace("/login");
          }}
          className="glow-ring mt-4 flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2 text-[13px] font-medium text-muted transition-colors hover:border-border-strong hover:text-foreground"
        >
          <LogoutIcon size={16} className="text-muted-2" />
          Log out
        </button>
      </aside>
      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-8 py-8">{children}</div>
      </main>
    </div>
  );
}
