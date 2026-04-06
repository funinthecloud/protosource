import {
  type DescMessage,
  type MessageInitShape,
  type MessageShape,
  create,
  toBinary,
  fromBinary,
  toJson,
  fromJson,
} from "@bufbuild/protobuf";
import type { AuthProvider } from "./auth.js";
import { parseAPIError } from "./errors.js";
import type { CommandResponse } from "./gen/response_v1_pb.js";
import { CommandResponseSchema } from "./gen/response_v1_pb.js";
import type { History } from "./gen/history_v1_pb.js";
import { HistorySchema } from "./gen/history_v1_pb.js";

export interface ClientOptions {
  /** Use JSON serialization instead of protobuf binary. */
  useJSON?: boolean;
  /** Override the global fetch function (useful for testing). */
  fetch?: typeof globalThis.fetch;
}

/**
 * ProtosourceClient is a generic HTTP client with content negotiation.
 * Generated per-aggregate clients delegate to this class.
 */
export class ProtosourceClient {
  private readonly baseURL: string;
  private readonly auth: AuthProvider;
  private readonly useJSON: boolean;
  private readonly fetch: typeof globalThis.fetch;

  constructor(baseURL: string, auth: AuthProvider, opts?: ClientOptions) {
    let url = baseURL;
    while (url.endsWith("/")) url = url.slice(0, -1);
    this.baseURL = url;
    this.auth = auth;
    this.useJSON = opts?.useJSON ?? false;
    this.fetch = opts?.fetch ?? globalThis.fetch.bind(globalThis);
  }

  /**
   * Send a command to the server.
   * The actor field is set from the AuthProvider before serialization.
   */
  async apply<Desc extends DescMessage>(
    routePath: string,
    schema: Desc,
    data: MessageInitShape<Desc>,
  ): Promise<CommandResponse> {
    const msg = create(schema, data);
    setActorField(msg, this.auth.actor());

    const cmdName = schema.typeName.split(".").pop()!.toLowerCase();
    const url = `${this.baseURL}/${routePath}/${cmdName}`;

    const headers = new Headers();
    this.auth.authenticate(headers);

    let body: BodyInit;
    let accept: string;
    if (this.useJSON) {
      body = JSON.stringify(toJson(schema, msg));
      headers.set("Content-Type", "application/json");
      accept = "application/json";
    } else {
      body = toBinary(schema, msg);
      headers.set("Content-Type", "application/protobuf");
      accept = "application/protobuf";
    }
    headers.set("Accept", accept);

    const resp = await this.fetch(url, { method: "POST", headers, body });

    if (!resp.ok) {
      const text = await resp.text();
      throw parseAPIError(resp.status, text);
    }

    const contentType = resp.headers.get("Content-Type") ?? "";
    if (contentType.includes("json")) {
      const text = await resp.text();
      return fromJson(CommandResponseSchema, JSON.parse(text)) as CommandResponse;
    }
    const buf = await resp.arrayBuffer();
    return fromBinary(CommandResponseSchema, new Uint8Array(buf)) as CommandResponse;
  }

  /** Retrieve the current state of an aggregate by ID via event replay. */
  async load<Desc extends DescMessage>(
    routePath: string,
    id: string,
    schema: Desc,
  ): Promise<MessageShape<Desc>> {
    const url = `${this.baseURL}/${routePath}/${encodeURIComponent(id)}`;
    return this.getProto(url, schema);
  }

  /** Retrieve an aggregate by ID from the materialized store. */
  async get<Desc extends DescMessage>(
    routePath: string,
    id: string,
    schema: Desc,
  ): Promise<MessageShape<Desc>> {
    const url = `${this.baseURL}/${routePath}/get/${encodeURIComponent(id)}`;
    return this.getProto(url, schema);
  }

  /** Retrieve the full event history for an aggregate. */
  async history(routePath: string, id: string): Promise<History> {
    const url = `${this.baseURL}/${routePath}/${encodeURIComponent(id)}/history`;
    return this.getProto(url, HistorySchema) as Promise<History>;
  }

  /** Query aggregates via a GSI index. */
  async query<Desc extends DescMessage>(
    routePath: string,
    queryPath: string,
    params: Record<string, string>,
    schema: Desc,
  ): Promise<MessageShape<Desc>> {
    const searchParams = new URLSearchParams(params);
    const url = `${this.baseURL}/${routePath}/query/${queryPath}?${searchParams.toString()}`;
    return this.getProto(url, schema);
  }

  private async getProto<Desc extends DescMessage>(
    url: string,
    schema: Desc,
  ): Promise<MessageShape<Desc>> {
    const headers = new Headers();
    headers.set("Accept", this.useJSON ? "application/json" : "application/protobuf");
    this.auth.authenticate(headers);

    const resp = await this.fetch(url, { method: "GET", headers });

    if (!resp.ok) {
      const text = await resp.text();
      throw parseAPIError(resp.status, text);
    }

    const contentType = resp.headers.get("Content-Type") ?? "";
    if (contentType.includes("json")) {
      const text = await resp.text();
      return fromJson(schema, JSON.parse(text)) as MessageShape<Desc>;
    }
    const buf = await resp.arrayBuffer();
    return fromBinary(schema, new Uint8Array(buf)) as MessageShape<Desc>;
  }
}

/**
 * Set the actor field (field number 2) on a command message.
 * All commands have `actor` as field 2 per protosource convention.
 */
function setActorField(msg: Record<string, unknown>, actor: string): void {
  // protoc-gen-es v2 generates plain object shapes with camelCase fields.
  // The actor field is always named "actor" in proto, which is "actor" in camelCase.
  msg["actor"] = actor;
}
