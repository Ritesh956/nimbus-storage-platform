"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api } from "./api";

interface AuthContextValue {
  // null = not yet determined (first render, before the refresh-cookie bootstrap resolves)
  isAuthenticated: boolean | null;
  // Resolves with a challenge when the account has TOTP enabled; the login
  // page then collects a code and calls totpLogin to finish.
  login: (email: string, password: string) => Promise<{ totpRequired: boolean; challengeToken?: string }>;
  totpLogin: (challengeToken: string, code: string) => Promise<void>;
  register: (email: string, password: string, orgName?: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean | null>(null);

  useEffect(() => {
    // The refresh token lives in an httpOnly cookie now (roadmap #1) — this
    // module can no longer just check localStorage for "was logged in".
    // Instead it makes the same call request()'s 401 handler would: hit
    // /api/auth/refresh, which reads the cookie server-side and, if it's
    // valid, hands back a fresh access token to hold in memory. A cancelled
    // flag guards against a slow response landing after unmount (React 18
    // strict-mode double-invokes effects in dev).
    let cancelled = false;
    api.auth
      .bootstrap()
      .then(() => {
        if (!cancelled) setIsAuthenticated(true);
      })
      .catch(() => {
        if (!cancelled) setIsAuthenticated(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const result = await api.auth.login(email, password);
    if (!result.totpRequired) setIsAuthenticated(true);
    return result;
  }, []);

  const totpLogin = useCallback(async (challengeToken: string, code: string) => {
    await api.auth.totpLogin(challengeToken, code);
    setIsAuthenticated(true);
  }, []);

  const register = useCallback(async (email: string, password: string, orgName?: string) => {
    await api.auth.register(email, password, orgName);
  }, []);

  const logout = useCallback(async () => {
    try {
      // api.auth.logout()'s own finally clears the in-memory access token
      // even if the network call itself fails — nothing left to do here.
      await api.auth.logout();
    } catch {
      // best-effort — see above
    }
    setIsAuthenticated(false);
  }, []);

  return (
    <AuthContext.Provider value={{ isAuthenticated, login, totpLogin, register, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
