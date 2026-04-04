/**
 * APIError represents an error response from the protosource server.
 * The server sends {"code": "...", "error": "...", "detail": "..."}.
 */
export class APIError extends Error {
  readonly statusCode: number;
  readonly code: string;
  readonly detail: string;

  constructor(statusCode: number, code: string, message: string, detail: string) {
    const full = detail
      ? `protosource: ${statusCode} ${code}: ${message} (${detail})`
      : `protosource: ${statusCode} ${code}: ${message}`;
    super(full);
    this.name = "APIError";
    this.statusCode = statusCode;
    this.code = code;
    this.detail = detail;
  }
}

interface ErrorBody {
  code?: string;
  error?: string;
  detail?: string;
}

export function parseAPIError(statusCode: number, body: string): APIError {
  try {
    const parsed: ErrorBody = JSON.parse(body);
    return new APIError(
      statusCode,
      parsed.code ?? "UNKNOWN",
      parsed.error ?? body,
      parsed.detail ?? "",
    );
  } catch {
    return new APIError(statusCode, "UNKNOWN", body, "");
  }
}
