"use client";

import { FormEvent, useState } from "react";
import useSWR from "swr";
import QRCode from "qrcode";
import { api, ApiError } from "@/lib/api";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Card } from "@/components/ui/Card";
import { PageHeader } from "@/components/ui/PageHeader";
import { CopyIcon, ShieldIcon } from "@/components/ui/Icons";

// Account security settings. 2FA is a user-level concern, not org-level —
// this page just lives under the org shell because that's where the nav is.
export default function SecurityPage() {
  const { data: status, mutate } = useSWR("totp-status", () => api.auth.totpStatus());

  // Pending enrollment state (after "Enable" is clicked, before confirm).
  const [setup, setSetup] = useState<{ secret: string; uri: string; qr: string } | null>(null);
  const [code, setCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [disabling, setDisabling] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [secretCopied, setSecretCopied] = useState(false);

  async function startSetup() {
    setBusy(true);
    setError(null);
    try {
      const res = await api.auth.totpSetup();
      const qr = await QRCode.toDataURL(res.otpauth_uri, { margin: 1, width: 192 });
      setSetup({ secret: res.secret, uri: res.otpauth_uri, qr });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "could not start enrollment");
    } finally {
      setBusy(false);
    }
  }

  async function confirm(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.auth.totpConfirm(code.trim());
      setSetup(null);
      setCode("");
      await mutate();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "could not confirm code");
    } finally {
      setBusy(false);
    }
  }

  async function disable(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.auth.totpDisable(disableCode.trim());
      setDisabling(false);
      setDisableCode("");
      await mutate();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "could not disable 2FA");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Security"
        description="Two-factor authentication for your account — it applies to your login everywhere, not just this organization."
      />

      <Card className="p-5">
        <div className="flex flex-wrap items-center gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-deep text-accent-2">
            <ShieldIcon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-sm font-medium">
              Authenticator app (TOTP)
              {status && (
                <Badge tone={status.enabled ? "success" : "neutral"}>
                  {status.enabled ? "enabled" : "off"}
                </Badge>
              )}
            </div>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-2">
              Six-digit codes from Google Authenticator, 1Password, Aegis, or any TOTP app. Once enabled,
              signing in takes your password plus a current code.
            </p>
          </div>
          {status && !status.enabled && !setup && (
            <Button disabled={busy} onClick={startSetup}>
              Enable 2FA
            </Button>
          )}
          {status?.enabled && !disabling && (
            <Button variant="danger" onClick={() => setDisabling(true)}>
              Disable
            </Button>
          )}
        </div>

        {setup && (
          <div className="mt-5 flex flex-col gap-4 border-t border-border/60 pt-5 sm:flex-row sm:items-start">
            {/* eslint-disable-next-line @next/next/no-img-element -- data URL QR, nothing to optimize */}
            <img
              src={setup.qr}
              alt="TOTP enrollment QR code"
              className="size-40 shrink-0 rounded-lg border border-border bg-white p-2"
            />
            <div className="min-w-0 flex-1">
              <p className="text-xs leading-relaxed text-muted">
                Scan the QR code with your authenticator app, or add the secret manually:
              </p>
              <div className="mt-2 flex items-center gap-2 rounded-lg border border-border bg-surface-deep px-3 py-2">
                <code className="min-w-0 flex-1 truncate text-xs tracking-wider text-muted">{setup.secret}</code>
                <Button
                  variant="ghost"
                  className="shrink-0"
                  onClick={async () => {
                    await navigator.clipboard.writeText(setup.secret);
                    setSecretCopied(true);
                  }}
                >
                  <CopyIcon size={13} />
                  {secretCopied ? "Copied" : "Copy"}
                </Button>
              </div>
              <form onSubmit={confirm} className="mt-3 flex items-center gap-2">
                <Input
                  required
                  autoFocus
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  placeholder="123456"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  className="max-w-32 text-center tracking-[0.3em]"
                />
                <Button type="submit" disabled={busy || code.trim().length !== 6}>
                  {busy ? "Verifying…" : "Verify & enable"}
                </Button>
                <Button variant="secondary" onClick={() => setSetup(null)}>
                  Cancel
                </Button>
              </form>
            </div>
          </div>
        )}

        {disabling && (
          <form
            onSubmit={disable}
            className="mt-5 flex flex-wrap items-center gap-2 border-t border-border/60 pt-5"
          >
            <span className="w-full text-xs text-muted sm:w-auto">
              Enter a current code to confirm turning 2FA off:
            </span>
            <Input
              required
              autoFocus
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              placeholder="123456"
              value={disableCode}
              onChange={(e) => setDisableCode(e.target.value)}
              className="max-w-32 text-center tracking-[0.3em]"
            />
            <Button type="submit" variant="danger" disabled={busy || disableCode.trim().length !== 6}>
              {busy ? "Disabling…" : "Disable 2FA"}
            </Button>
            <Button variant="secondary" onClick={() => setDisabling(false)}>
              Cancel
            </Button>
          </form>
        )}

        {error && <p className="mt-3 text-xs text-danger">{error}</p>}
      </Card>
    </div>
  );
}
