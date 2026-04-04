/** Result of a successful command application. */
export interface ApplyResult {
  id: string;
  version: number;
}

/** A single event record from aggregate history. */
export interface HistoryRecord {
  version: number;
  /** Base64-encoded serialized event bytes. */
  data: string;
  ttl: number;
}

/** Full event history for an aggregate. */
export interface History {
  records: HistoryRecord[];
}
