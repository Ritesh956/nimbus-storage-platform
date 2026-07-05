"use client";

import { FormEvent, Suspense, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { LogoMark } from "@/components/ui/Icons";

// useSearchParams needs a Suspense boundary (the token arrives as
// /reset-password?token=... from the email link), hence the split into a
// wrapper page + inner form component.
export default function ResetPasswordPage() {
  return (
    <Suspense>
      <ResetPasswordForm />
    </Suspense>
  );
}

function ResetPasswordForm() {
  const router = useRouter();
  const token = useSearchParams().get("token") ?? "";
  const [password, setPassword] = useState("");
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await api.auth.resetPassword(token, password);
      setDone(true);
      setTimeout(() => router.replace("/login"), 2500);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "password reset failed");
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
            <h1 className="text-xl font-semibold tracking-tight">Choose a new password</h1>
            <p className="mt-1 text-xs text-muted">
              {done ? "All set — taking you to sign in" : "Your old sessions will be signed out everywhere"}
            </p>
          </div>
        </div>
        <Card className="p-6">
          {done ? (
            <p className="text-sm leading-relaxed text-muted">
              Password updated. <Link href="/login" className="font-medium text-accent hover:underline">Sign in</Link>{" "}
              with your new password.
            </p>
          ) : !token ? (
            <p className="text-sm leading-relaxed text-muted">
              This page needs the reset link from your email — the token is missing.{" "}
              <Link href="/forgot-password" className="font-medium text-accent hover:underline">
                Request a new link
              </Link>
              .
            </p>
          ) : (
            <form onSubmit={onSubmit} className="flex flex-col gap-4">
              <div>
                <label className="mb-1.5 block text-xs font-medium text-muted">New password</label>
                <Input
                  type="password"
                  required
                  autoFocus
                  minLength={8}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <p className="mt-1.5 text-[11px] text-muted-2">At least 8 characters.</p>
              </div>
              {error && <p className="text-xs text-danger">{error}</p>}
              <Button type="submit" disabled={loading} className="mt-1 w-full py-2.5 text-sm">
                {loading ? "Saving…" : "Set new password"}
              </Button>
            </form>
          )}
        </Card>
      </div>
    </div>
  );
}
