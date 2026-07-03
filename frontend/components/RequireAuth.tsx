"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (isAuthenticated === false) router.replace("/login");
  }, [isAuthenticated, router]);

  if (!isAuthenticated) return null; // covers both "still checking" and "redirecting"
  return <>{children}</>;
}
