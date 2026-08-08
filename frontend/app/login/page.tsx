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
  const { login, totpLogin } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  // Non-null challenge = password step passed, waiting on the TOTP code.
  const [challenge, setChallenge] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const result = await login(email, password);
      if (result.totpRequired && result.challengeToken) {
        setChallenge(result.challengeToken);
        return;
      }
      router.replace("/app");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "login failed");
    } finally {
      setLoading(false);
    }
  }

  async function onSubmitCode(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await totpLogin(challenge!, code.trim());
      router.replace("/app");
    } catch (err) {
      if (err instanceof ApiError && err.message.includes("challenge")) {
        // Challenge expired (5 min TTL) — back to the password step.
        setChallenge(null);
        setCode("");
        setError("That took too long — please sign in again.");
      } else {
        setError(err instanceof ApiError ? err.message : "login failed");
      }
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
            <h1 className="text-xl font-semibold tracking-tight">
              {challenge ? "Two-factor authentication" : "Welcome back"}
            </h1>
            <p className="mt-1 text-xs text-muted">
              {challenge
                ? "Enter the 6-digit code from your authenticator app"
                : "Sign in to your Nimbus workspace"}
            </p>
          </div>
        </div>
        <Card className="p-6">
          {challenge ? (
            <form onSubmit={onSubmitCode} className="flex flex-col gap-4">
              <div>
                <label htmlFor="totp-code" className="mb-1.5 block text-xs font-medium text-muted">
                  Authentication code
                </label>
                <Input
                  id="totp-code"
                  required
                  autoFocus
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  placeholder="123456"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  className="text-center text-lg tracking-[0.4em]"
                />
              </div>
              {error && (
                <p role="alert" className="text-xs text-danger">
                  {error}
                </p>
              )}
              <Button type="submit" disabled={loading || code.trim().length !== 6} className="mt-1 w-full py-2.5 text-sm">
                {loading ? "Verifying…" : "Verify"}
              </Button>
            </form>
          ) : (
            <form onSubmit={onSubmit} className="flex flex-col gap-4">
              <div>
                <label htmlFor="login-email" className="mb-1.5 block text-xs font-medium text-muted">
                  Email
                </label>
                <Input
                  id="login-email"
                  type="email"
                  required
                  autoFocus
                  placeholder="you@company.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div>
                <div className="mb-1.5 flex items-center justify-between">
                  <label htmlFor="login-password" className="block text-xs font-medium text-muted">
                    Password
                  </label>
                  <Link href="/forgot-password" className="glow-ring rounded text-[11px] text-accent hover:underline">
                    Forgot password?
                  </Link>
                </div>
                <Input
                  id="login-password"
                  type="password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error && (
                <p role="alert" className="text-xs text-danger">
                  {error}
                </p>
              )}
              <Button type="submit" disabled={loading} className="mt-1 w-full py-2.5 text-sm">
                {loading ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          )}
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
