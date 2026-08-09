"use client";

import { useSyncExternalStore } from "react";
import { applyTheme, getEffectiveTheme, subscribeToThemeChanges, ThemePreference } from "@/lib/theme";
import { MoonIcon, SunIcon } from "./ui/Icons";

// useSyncExternalStore, not useState+useEffect: this needs a value that
// only exists client-side (localStorage, matchMedia) without either a
// hydration mismatch (server has no theme preference to read) or the
// setState-in-effect anti-pattern react-hooks/set-state-in-effect flags —
// this hook is React's own answer to exactly that combination. The server
// snapshot matches globals.css's un-overridden dark baseline, so the very
// first client paint (before this re-syncs) never visibly disagrees with
// what the server already sent.
function getServerSnapshot(): ThemePreference {
  return "dark";
}

// A toggle, not a 3-way system/light/dark picker — audit §11 asked for a
// real light theme to exist and be reachable, not a full preferences
// surface. Clicking always sets an explicit override (lib/theme.ts); there
// isn't a "back to system" affordance here on purpose, matching the
// minimal-scope fix the audit's own note asked for.
export function ThemeToggle() {
  const effective = useSyncExternalStore(subscribeToThemeChanges, getEffectiveTheme, getServerSnapshot);
  const next: ThemePreference = effective === "dark" ? "light" : "dark";

  return (
    <button
      onClick={() => applyTheme(next)}
      title={`Switch to ${next} theme`}
      aria-label={`Switch to ${next} theme`}
      className="glow-ring rounded-lg p-2 text-muted-2 transition-colors hover:bg-surface-deep hover:text-foreground"
    >
      {effective === "dark" ? <SunIcon size={16} /> : <MoonIcon size={16} />}
    </button>
  );
}
