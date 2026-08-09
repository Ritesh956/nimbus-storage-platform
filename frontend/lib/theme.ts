// Explicit theme override (audit §11: a real light theme, not just an
// assumed dark-only one). Absence of a stored value means "follow the OS"
// — app/globals.css's `@media (prefers-color-scheme: light)` block already
// handles that half; this module only exists for the times a user
// explicitly disagrees with their OS (dark OS, wants light in this app, or
// vice versa).
const STORAGE_KEY = "nimbus-theme";

export type ThemePreference = "dark" | "light";

export function getStoredTheme(): ThemePreference | null {
  if (typeof window === "undefined") return null;
  const v = window.localStorage.getItem(STORAGE_KEY);
  return v === "dark" || v === "light" ? v : null;
}

// Fires whenever applyTheme runs in *this* tab — the native "storage"
// event only fires in *other* tabs/windows, so components reading theme
// state via useSyncExternalStore (components/ThemeToggle.tsx) need this to
// know a same-tab click changed anything.
const CHANGE_EVENT = "nimbus-theme-change";

// Applies (or clears) the override on <html data-theme="...">, which
// globals.css's :root[data-theme="dark"|"light"] blocks read — those beat
// the prefers-color-scheme media query per normal CSS specificity/source
// order, so this always wins over the OS setting once set.
export function applyTheme(theme: ThemePreference | null) {
  if (typeof document === "undefined") return;
  if (theme) {
    document.documentElement.dataset.theme = theme;
    window.localStorage.setItem(STORAGE_KEY, theme);
  } else {
    delete document.documentElement.dataset.theme;
    window.localStorage.removeItem(STORAGE_KEY);
  }
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

// Subscribes to every signal that can change the *effective* theme: an
// explicit same-tab applyTheme() call, a stored preference changing in
// another tab, or the OS-level preference itself flipping while no
// override is set. For useSyncExternalStore (components/ThemeToggle.tsx).
export function subscribeToThemeChanges(callback: () => void): () => void {
  const media = window.matchMedia("(prefers-color-scheme: light)");
  window.addEventListener(CHANGE_EVENT, callback);
  window.addEventListener("storage", callback);
  media.addEventListener("change", callback);
  return () => {
    window.removeEventListener(CHANGE_EVENT, callback);
    window.removeEventListener("storage", callback);
    media.removeEventListener("change", callback);
  };
}

export function getEffectiveTheme(): ThemePreference {
  return getStoredTheme() ?? (window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
}

// Inlined into app/layout.tsx's <head> as a blocking <script> (not run
// through this module — it has to execute before first paint, which an
// imported/bundled function can't guarantee) so the stored preference
// applies before the browser paints the default (dark) tokens, avoiding a
// light-theme user seeing one dark frame on every load. Kept here as the
// single source of truth for what that inline script does, in sync with
// STORAGE_KEY above rather than a second hand-copied string.
export const themeInitScript = `(function(){try{var t=localStorage.getItem(${JSON.stringify(STORAGE_KEY)});if(t==="dark"||t==="light")document.documentElement.dataset.theme=t;}catch(e){}})();`;
