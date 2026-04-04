/** AuthProvider decorates outgoing HTTP requests with authentication. */
export interface AuthProvider {
  authenticate(headers: Headers): void;
  actor(): string;
}

/** BearerTokenAuth adds a Bearer token to each request. */
export class BearerTokenAuth implements AuthProvider {
  private readonly token: string;
  private readonly _actor: string;

  constructor(token: string, actor: string) {
    this.token = token;
    this._actor = actor;
  }

  authenticate(headers: Headers): void {
    headers.set("Authorization", "Bearer " + this.token);
  }

  actor(): string {
    return this._actor;
  }
}

/** NoAuth provides actor identity without authentication headers. */
export class NoAuth implements AuthProvider {
  private readonly _actor: string;

  constructor(actor: string) {
    this._actor = actor;
  }

  authenticate(_headers: Headers): void {}

  actor(): string {
    return this._actor;
  }
}
