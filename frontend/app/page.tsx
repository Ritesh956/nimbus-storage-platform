"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";

export default function RootPage() {
  const { isAuthenticated } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (isAuthenticated === null) return; // still waiting on the refresh-cookie bootstrap
    router.replace(isAuthenticated ? "/app" : "/login");
  }, [isAuthenticated, router]);

  return null;
}
