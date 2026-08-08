import { describe, it, expect, vi, afterEach } from "vitest";
import { formatBytes, formatDate, timeAgo } from "./format";

// Audit §14/§10: frontend had zero automated tests of any kind. format.ts's
// unit conversion and relative-time math are pure functions with real edge
// cases (the 0-byte / null special case, the log-base-1024 unit selection)
// worth pinning down explicitly.

describe("formatBytes", () => {
  it("renders null/undefined as an em dash", () => {
    expect(formatBytes(null)).toBe("—");
    expect(formatBytes(undefined)).toBe("—");
  });

  it("renders exactly 0 bytes as '0 B', not '0.0 B'", () => {
    expect(formatBytes(0)).toBe("0 B");
  });

  it("keeps whole-number precision for bytes", () => {
    expect(formatBytes(512)).toBe("512 B");
  });

  it("uses one decimal place once it crosses into KB", () => {
    expect(formatBytes(1536)).toBe("1.5 KB");
  });

  it("selects MB/GB/TB units at the right magnitude", () => {
    expect(formatBytes(1024 * 1024)).toBe("1.0 MB");
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1.0 GB");
    expect(formatBytes(1024 * 1024 * 1024 * 1024)).toBe("1.0 TB");
  });
});

describe("timeAgo", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reports 'just now' for anything under a minute old", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:30Z"));
    expect(timeAgo("2026-01-01T00:00:00Z")).toBe("just now");
  });

  it("reports whole minutes once past 60 seconds", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:05:00Z"));
    expect(timeAgo("2026-01-01T00:00:00Z")).toBe("5m ago");
  });

  it("reports whole hours once past 60 minutes", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T03:00:00Z"));
    expect(timeAgo("2026-01-01T00:00:00Z")).toBe("3h ago");
  });

  it("reports whole days once past 24 hours", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-05T00:00:00Z"));
    expect(timeAgo("2026-01-01T00:00:00Z")).toBe("4d ago");
  });
});

describe("formatDate", () => {
  it("produces a non-empty, locale-formatted string for a valid ISO date", () => {
    const result = formatDate("2026-03-05T12:30:00Z");
    expect(result.length).toBeGreaterThan(0);
    expect(result).toMatch(/2026/);
  });
});
