/**
 * Current time as Unix microseconds, matching protosource.NowMicros in Go.
 * Use this when constructing values that the server will compare against
 * its own NowMicros-stamped fields.
 */
export function nowMicros(): bigint {
  return BigInt(Date.now()) * 1000n;
}

/**
 * Convert a Unix-microsecond timestamp (as emitted by NowMicros and stamped
 * onto create_at / modify_at by the framework) to a JS Date.
 *
 * Note: Date has millisecond resolution, so the sub-millisecond portion of
 * `us` is truncated. For uses that need full precision (audit trails, event
 * ordering), keep the bigint and compare directly.
 */
export function fromMicros(us: bigint | number): Date {
  return new Date(Number(BigInt(us) / 1000n));
}
