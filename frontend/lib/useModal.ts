// Shared modal a11y contract for every overlay dialog (ConfirmDialog,
// ShareDialog, MoveDialog, FileRow's PreviewModal): move focus into the
// panel on open, trap Tab/Shift+Tab inside it so the page behind never
// receives focus while the modal is up, close on Escape, and return focus
// to whatever triggered the modal on close. This was previously
// copy-pasted per modal as an Escape-only keydown effect with no focus
// trap; consolidated here so all four stay in sync.
import { useEffect, useRef } from "react";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function useModal<T extends HTMLElement>(onClose: () => void) {
  const panelRef = useRef<T>(null);

  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    const focusables = () => (panel ? Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)) : []);

    // Nothing inside the panel is focusable yet on the very first paint of
    // some dialogs (e.g. a share dialog before its "Create link" button
    // exists) — fall back to the panel itself, which carries tabIndex={-1}.
    (focusables()[0] ?? panel)?.focus();

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key !== "Tab") return;
      const items = focusables();
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }

    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      previouslyFocused?.focus();
    };
    // onClose is intentionally the only dep — panelRef is stable and
    // re-running this on every render would refocus the panel mid-interaction.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onClose]);

  return panelRef;
}
