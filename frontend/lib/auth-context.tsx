"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api } from "./api";
import { clearTokens, getRefreshToken } from "./tokens";

interface AuthContextValue {
  // null = not yet determined (first render, before we've checked storage)
  isAuthenticated: boolean | null;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState<boolean | null>(null);

  useEffect(() => {
    // Presence of a refresh token is "was logged in" — the access token
    // itself may have expired, but lib/api.ts's request() transparently
    // refreshes on the first 401, so this is a safe proxy for auth state.
    // localStorage doesn't exist during SSR, so this genuinely can't be a
    // useState lazy initializer (that would read window on the server and
    // either throw or desync from the client's hydration pass) — the
    // standard "resolve client-only state after mount" pattern, hence the
    // deliberate one-time setState-in-effect.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setIsAuthenticated(!!getRefreshToken());
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    await api.auth.login(email, password);
    setIsAuthenticated(true);
  }, []);

  const register = useCallback(async (email: string, password: string) => {
    await api.auth.register(email, password);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.auth.logout();
    } catch {
      clearTokens();
    }
    setIsAuthenticated(false);
  }, []);

  return (
    <AuthContext.Provider value={{ isAuthenticated, login, register, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
