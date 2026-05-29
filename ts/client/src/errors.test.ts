import { describe, expect, it } from "vitest";
import { create, toBinary, toJson } from "@bufbuild/protobuf";
import { ErrorSchema } from "./gen/funinthecloud/protosource/apierror/v1/apierror_v1_pb.js";
import { APIError, parseAPIError } from "./errors.js";

const enc = new TextEncoder();

describe("parseAPIError", () => {
  it("decodes a protobuf-binary error body", () => {
    const body = toBinary(
      ErrorSchema,
      create(ErrorSchema, { code: "CMD_VALIDATION", message: "bad input", detail: "field x" }),
    );
    const err = parseAPIError(400, "application/protobuf", body);
    expect(err).toBeInstanceOf(APIError);
    expect(err.statusCode).toBe(400);
    expect(err.code).toBe("CMD_VALIDATION");
    expect(err.message).toContain("bad input");
    expect(err.detail).toBe("field x");
  });

  it("decodes a JSON error body", () => {
    const json = JSON.stringify(
      toJson(ErrorSchema, create(ErrorSchema, { code: "GET_NOT_FOUND", message: "missing" })),
    );
    const err = parseAPIError(404, "application/json", enc.encode(json));
    expect(err.statusCode).toBe(404);
    expect(err.code).toBe("GET_NOT_FOUND");
    expect(err.message).toContain("missing");
  });

  it("falls back to UNKNOWN for a non-proto body (e.g. an HTML gateway page)", () => {
    const html = enc.encode("<html><body>502 Bad Gateway</body></html>");
    const err = parseAPIError(502, "text/html", html);
    expect(err.code).toBe("UNKNOWN");
    expect(err.message).toContain("502 Bad Gateway");
  });

  it("falls back to UNKNOWN when JSON is malformed", () => {
    const err = parseAPIError(503, "application/json", enc.encode("not json"));
    expect(err.code).toBe("UNKNOWN");
    expect(err.message).toContain("not json");
  });
});
