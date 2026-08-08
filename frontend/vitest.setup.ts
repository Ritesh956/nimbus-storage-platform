import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// vitest.config.mts doesn't set test.globals: true (deliberately — explicit
// describe/it/expect imports per file), so React Testing Library's own
// auto-cleanup (which only registers itself when it finds afterEach on
// globalThis) never fires — every component test after the first would see
// the previous test's still-mounted DOM. Registering it here explicitly is
// the officially documented non-globals workaround.
afterEach(() => {
  cleanup();
});
