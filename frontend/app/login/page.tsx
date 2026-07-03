"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { ApiError } from "@/lib/api";
import { Card, EyebrowLabel } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";

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
      <Card className="w-full max-w-sm">
        <EyebrowLabel>Nimbus</EyebrowLabel>
        <h1 className="mt-1 text-2xl font-semibold">Sign in</h1>
        <form onSubmit={onSubmit} className="mt-6 flex flex-col gap-4">
          <div>
            <label className="mb-1 block text-sm text-muted">Email</label>
            <Input type="email" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-muted">Password</label>
            <Input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          {error && <p className="text-sm text-danger">{error}</p>}
          <Button type="submit" disabled={loading} className="w-full">
            {loading ? "Signing in…" : "Sign in"}
          </Button>
        </form>
        <p className="mt-5 text-center text-sm text-muted">
          No account?{" "}
          <Link href="/register" className="text-accent hover:underline">
            Register
          </Link>
        </p>
      </Card>
    </div>
  );
}
