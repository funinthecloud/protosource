import { describe, expect, it } from "vitest";
import { fromMicros, nowMicros } from "./time.js";

describe("nowMicros", () => {
  it("returns a bigint within ±1s of Date.now() * 1000", () => {
    const before = BigInt(Date.now()) * 1000n;
    const got = nowMicros();
    const after = BigInt(Date.now()) * 1000n;
    expect(typeof got).toBe("bigint");
    expect(got >= before - 1_000_000n).toBe(true);
    expect(got <= after + 1_000_000n).toBe(true);
  });
});

describe("fromMicros", () => {
  it("converts 0n to the Unix epoch", () => {
    expect(fromMicros(0n).toISOString()).toBe("1970-01-01T00:00:00.000Z");
  });

  it("accepts a number input", () => {
    expect(fromMicros(0).toISOString()).toBe("1970-01-01T00:00:00.000Z");
  });

  it("round-trips with nowMicros within 1ms", () => {
    const now = new Date();
    const decoded = fromMicros(nowMicros());
    expect(Math.abs(decoded.getTime() - now.getTime())).toBeLessThanOrEqual(1);
  });

  it("truncates sub-millisecond precision", () => {
    // 1_500us = 1ms + 500us → JS Date should round down to 1ms.
    expect(fromMicros(1_500n).getTime()).toBe(1);
  });
});
