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

export default function RegisterPage() {
  const { register, login } = useAuth();
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
      await register(email, password);
      await login(email, password);
      router.replace("/app");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "registration failed");
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
            <h1 className="text-xl font-semibold tracking-tight">Create your account</h1>
            <p className="mt-1 text-xs text-muted">Distributed storage for your team</p>
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
              <Input
                type="password"
                required
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <p className="mt-1.5 text-[11px] text-muted-2">At least 8 characters.</p>
            </div>
            {error && <p className="text-xs text-danger">{error}</p>}
            <Button type="submit" disabled={loading} className="mt-1 w-full py-2.5 text-sm">
              {loading ? "Creating account…" : "Create account"}
            </Button>
          </form>
        </Card>
        <p className="mt-5 text-center text-xs text-muted">
          Already have an account?{" "}
          <Link href="/login" className="glow-ring rounded font-medium text-accent hover:underline">
            Sign in
          </Link>
        </p>
      </div>
    </div>
  );
}
