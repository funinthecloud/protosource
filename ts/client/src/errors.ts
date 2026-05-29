import { fromBinary, fromJson } from "@bufbuild/protobuf";
// Import only ErrorSchema (a value) — NOT the generated `Error` type, which
// would shadow the global Error that APIError extends.
import { ErrorSchema } from "./gen/funinthecloud/protosource/apierror/v1/apierror_v1_pb.js";

/**
 * APIError represents an error response from the protosource server. The wire
 * body is an apierror.v1.Error (protobuf binary by default, JSON in debug
 * mode); statusCode comes from the HTTP status line, not the body.
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

/**
 * parseAPIError decodes an error response body into an APIError. The body is an
 * apierror.v1.Error, content-negotiated like every other message; contentType
 * (from the response's Content-Type header) selects protobuf or JSON decoding.
 * Bodies that are not a valid Error — e.g. a plaintext 502 from a load balancer
 * or an HTML gateway page — fall back to a synthetic UNKNOWN error carrying the
 * raw body text as the message.
 */
export function parseAPIError(
  statusCode: number,
  contentType: string,
  body: Uint8Array,
): APIError {
  try {
    const wire = contentType.includes("json")
      ? fromJson(ErrorSchema, JSON.parse(new TextDecoder().decode(body)))
      : fromBinary(ErrorSchema, body);
    if (!wire.code) {
      return new APIError(statusCode, "UNKNOWN", new TextDecoder().decode(body), "");
    }
    return new APIError(statusCode, wire.code, wire.message, wire.detail);
  } catch {
    return new APIError(statusCode, "UNKNOWN", new TextDecoder().decode(body), "");
  }
}
