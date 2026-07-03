"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { Button } from "./ui/Button";

const navItems = (orgId: string) => [
  { href: `/app/org/${orgId}`, label: "Files", match: `/app/org/${orgId}/folder` },
  { href: `/app/org/${orgId}/search`, label: "Search" },
  { href: `/app/org/${orgId}/trash`, label: "Trash" },
  { href: `/app/org/${orgId}/activity`, label: "Activity" },
  { href: `/app/org/${orgId}/admin`, label: "Admin" },
];

export function AppShell({ orgId, orgName, children }: { orgId: string; orgName?: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { logout } = useAuth();

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 shrink-0 flex-col border-r border-border px-4 py-6">
        <Link href="/app" className="mb-8 px-2 text-lg font-semibold tracking-tight">
          <span className="text-accent">Nimbus</span>
        </Link>
        {orgName && <div className="mb-4 truncate px-2 text-xs text-muted">{orgName}</div>}
        <nav className="flex flex-1 flex-col gap-1">
          {navItems(orgId).map((item) => {
            const active = pathname === item.href || (item.match && pathname.startsWith(item.match));
            return (
              <Link
                key={item.href}
                href={item.href}
                className={`rounded-lg px-3 py-2 text-sm transition-colors ${
                  active ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-foreground"
                }`}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
        <Button
          variant="ghost"
          className="mt-4"
          onClick={async () => {
            await logout();
            router.replace("/login");
          }}
        >
          Log out
        </Button>
      </aside>
      <main className="flex-1 overflow-y-auto p-8">{children}</main>
    </div>
  );
}
