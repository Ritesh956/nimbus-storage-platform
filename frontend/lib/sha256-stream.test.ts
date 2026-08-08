import { describe, it, expect } from "vitest";
import { createHash } from "node:crypto";
import { Sha256Stream } from "./sha256-stream";

// Audit §14/next-session.md: Sha256Stream (a hand-rolled FIPS 180-4
// streaming SHA-256, needed because SubtleCrypto.digest has no incremental
// variant) was explicitly named as untested intricate client logic — a
// wrong digest here would silently corrupt every upload's integrity check.
// Node's own crypto.createHash('sha256') is the ground-truth oracle: if
// Sha256Stream disagrees with it for any input, the implementation is wrong,
// not the test.

function expectedHex(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function digestOf(chunks: Uint8Array[]): string {
  const s = new Sha256Stream();
  for (const c of chunks) s.update(c);
  return s.digestHex();
}

function randomBytes(n: number, seed = 1): Uint8Array {
  // Deterministic pseudo-random fill (no need for cryptographic randomness,
  // just varied byte content) so failures are reproducible.
  const out = new Uint8Array(n);
  let x = seed;
  for (let i = 0; i < n; i++) {
    x = (x * 1103515245 + 12345) & 0x7fffffff;
    out[i] = x % 256;
  }
  return out;
}

describe("Sha256Stream", () => {
  it("matches Node's crypto for empty input", () => {
    const empty = new Uint8Array(0);
    expect(digestOf([empty])).toBe(expectedHex(empty));
  });

  it("matches Node's crypto for a small single-chunk input", () => {
    const data = new TextEncoder().encode("abc");
    expect(digestOf([data])).toBe(expectedHex(data));
  });

  // FIPS 180-4 padding math branches on whether the final block's used
  // length is < 56 bytes (pad fits in the same block) or >= 56 (padding
  // spills into a second block) — 55/56/57 straddle that boundary exactly,
  // and 64/128 land exactly on a block boundary with zero pending bytes.
  it.each([0, 1, 55, 56, 57, 63, 64, 65, 128, 1000, 8 * 1024 * 1024 + 37])(
    "matches Node's crypto for a %i-byte input in one chunk",
    (n) => {
      const data = randomBytes(n, n + 1);
      expect(digestOf([data])).toBe(expectedHex(data));
    },
  );

  it("produces the same digest regardless of how the input is chunked across update() calls", () => {
    const data = randomBytes(50_000, 42);
    const want = expectedHex(data);

    // One call.
    expect(digestOf([data])).toBe(want);

    // Split into fixed 64-byte blocks (exactly the internal block size).
    const in64 = [];
    for (let i = 0; i < data.length; i += 64) in64.push(data.subarray(i, i + 64));
    expect(digestOf(in64)).toBe(want);

    // Split into irregular, non-block-aligned chunk sizes.
    const irregular = [];
    let i = 0;
    const sizes = [1, 3, 200, 7, 8191, 1, 4096];
    let s = 0;
    while (i < data.length) {
      const take = Math.min(sizes[s % sizes.length], data.length - i);
      irregular.push(data.subarray(i, i + take));
      i += take;
      s++;
    }
    expect(digestOf(irregular)).toBe(want);

    // Byte-by-byte, the most adversarial possible chunking.
    const byteByByte = Array.from(data.subarray(0, 500)).map((b) => new Uint8Array([b]));
    expect(digestOf(byteByByte)).toBe(expectedHex(data.subarray(0, 500)));
  });

  it("does not reuse state across separate instances", () => {
    const a = new Sha256Stream();
    a.update(new TextEncoder().encode("first"));
    const digestA = a.digestHex();

    const b = new Sha256Stream();
    b.update(new TextEncoder().encode("second"));
    const digestB = b.digestHex();

    expect(digestA).not.toBe(digestB);
    expect(digestA).toBe(expectedHex(new TextEncoder().encode("first")));
    expect(digestB).toBe(expectedHex(new TextEncoder().encode("second")));
  });
});
