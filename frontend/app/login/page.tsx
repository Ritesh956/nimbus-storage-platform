"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { ApiError } from "@/lib/api";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { LogoMark } from "@/components/ui/Icons";

export default function LoginPage() {
  const { login } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await login(email, password);
      router.replace("/app");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex flex-1 items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-3">
          <LogoMark size={44} />
          <div className="text-center">
            <h1 className="text-xl font-semibold tracking-tight">Welcome back</h1>
            <p className="mt-1 text-xs text-muted">Sign in to your Nimbus workspace</p>
          </div>
        </div>
        <Card className="p-6">
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-muted">Email</label>
              <Input
                type="email"
                required
                autoFocus
                placeholder="you@company.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs font-medium text-muted">Password</label>
              <Input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
            {error && <p className="text-xs text-danger">{error}</p>}
            <Button type="submit" disabled={loading} className="mt-1 w-full py-2.5 text-sm">
              {loading ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </Card>
        <p className="mt-5 text-center text-xs text-muted">
          No account?{" "}
          <Link href="/register" className="glow-ring rounded font-medium text-accent hover:underline">
            Register
          </Link>
        </p>
      </div>
    </div>
  );
}
