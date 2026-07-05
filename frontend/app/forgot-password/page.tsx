"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { api, ApiError } from "@/lib/api";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { LogoMark } from "@/components/ui/Icons";

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await api.auth.forgotPassword(email);
      setSent(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "request failed");
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
            <h1 className="text-xl font-semibold tracking-tight">Reset your password</h1>
            <p className="mt-1 text-xs text-muted">
              {sent
                ? "Check your inbox for the reset link"
                : "Enter your account email and we'll send you a reset link"}
            </p>
          </div>
        </div>
        <Card className="p-6">
          {sent ? (
            // The backend answers identically for unknown emails (no user
            // enumeration), so this is worded as "if an account exists".
            <p className="text-sm leading-relaxed text-muted">
              If an account exists for <span className="font-medium text-foreground">{email}</span>, a reset
              link is on its way. The link expires in 1 hour.
            </p>
          ) : (
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
              {error && <p className="text-xs text-danger">{error}</p>}
              <Button type="submit" disabled={loading} className="mt-1 w-full py-2.5 text-sm">
                {loading ? "Sending…" : "Send reset link"}
              </Button>
            </form>
          )}
        </Card>
        <p className="mt-5 text-center text-xs text-muted">
          Remembered it?{" "}
          <Link href="/login" className="glow-ring rounded font-medium text-accent hover:underline">
            Sign in
          </Link>
        </p>
      </div>
    </div>
  );
}
