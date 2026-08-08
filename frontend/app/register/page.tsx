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
  const [orgName, setOrgName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [accountCreated, setAccountCreated] = useState(false);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await register(email, password, orgName);
      // Account (and its default org) now exists even if the following
      // login call fails — a retry of this form would 409 on the
      // already-taken email, so point the user at /login instead of
      // making them think registration itself failed.
      setAccountCreated(true);
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
              <label htmlFor="register-email" className="mb-1.5 block text-xs font-medium text-muted">
                Email
              </label>
              <Input
                id="register-email"
                type="email"
                required
                autoFocus
                placeholder="you@company.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="register-password" className="mb-1.5 block text-xs font-medium text-muted">
                Password
              </label>
              <Input
                id="register-password"
                type="password"
                required
                minLength={8}
                aria-describedby="register-password-hint"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <p id="register-password-hint" className="mt-1.5 text-[11px] text-muted-2">
                At least 8 characters.
              </p>
            </div>
            <div>
              <label htmlFor="register-org-name" className="mb-1.5 block text-xs font-medium text-muted">
                Organization name
              </label>
              <Input
                id="register-org-name"
                type="text"
                maxLength={200}
                placeholder="Acme Inc"
                aria-describedby="register-org-hint"
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
              />
              <p id="register-org-hint" className="mt-1.5 text-[11px] text-muted-2">
                Optional — we&apos;ll name it for you if you skip this.
              </p>
            </div>
            {error && (
              <p role="alert" className="text-xs text-danger">
                {accountCreated ? (
                  <>
                    Account created, but sign-in failed: {error}. Try{" "}
                    <Link href="/login" className="glow-ring rounded font-medium text-accent hover:underline">
                      signing in
                    </Link>{" "}
                    instead.
                  </>
                ) : (
                  error
                )}
              </p>
            )}
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
