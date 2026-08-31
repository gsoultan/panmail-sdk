import { InvalidMessageError, RateLimitedError, TransportError, classify } from './errors.js';
import { type Message, type Result, type Status, toWire } from './message.js';

/**
 * Not Authorization on purpose. That header carries a dashboard session, and a
 * key sent as a bearer token is rejected as a malformed session rather than as
 * a bad key — a confusing way to learn you used the wrong header.
 */
const API_KEY_HEADER = 'X-API-Key';

/**
 * The Connect route for EmailService.SendEmail. Connect derives it from the
 * proto package and service name, so it changes only if the proto does.
 */
const SEND_PROCEDURE = '/panmail.v1.EmailService/SendEmail';

/**
 * Bounds a single send. Generous, because the gateway writes the message to
 * its outbox before answering and that is a disk write on a possibly busy
 * database — but finite, because a send that hangs holds whatever request is
 * waiting on it.
 */
export const DEFAULT_TIMEOUT_MS = 30_000;

/**
 * Bounds what a single response may cost in memory. A send response is a
 * message id and a status; anything approaching this is a proxy error page,
 * not the gateway.
 */
const MAX_RESPONSE_BYTES = 1 << 20;

export interface ClientOptions {
  /** Bounds each send, in milliseconds. Defaults to DEFAULT_TIMEOUT_MS. */
  timeoutMs?: number;

  /**
   * Waits out up to n rate-limit refusals, sleeping for the delay the gateway
   * asks for each time.
   *
   * Off by default, and only ever applied to a refusal. A refusal is the one
   * failure where the client knows the message was not accepted, so repeating
   * it cannot deliver anything twice — but it does mean send() blocks for as
   * long as the gateway asks, which inside a request handler is a decision the
   * caller should make rather than inherit. A queue of your own is usually the
   * better answer.
   */
  rateLimitRetries?: number;

  /**
   * Extra headers on every request — a tracing header, or what a proxy wants.
   *
   * A value carrying a carriage return or newline is refused: those end a
   * header line, so a value holding one is a second header the caller did not
   * write. A tracing header built out of something a user supplied is exactly
   * where that matters.
   */
  headers?: Record<string, string>;

  /** Supply your own fetch, for a proxy agent or a test double. */
  fetch?: typeof globalThis.fetch;
}

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/**
 * Sends mail through a panmail gateway.
 *
 * It talks to the same endpoint the web UI uses, authenticating with an API key
 * rather than a session. Create the key in Settings → API Keys with the
 * email:send scope; the key carries the tenant, so there is nothing else to
 * configure.
 *
 * ```ts
 * const client = new PanmailClient('https://mail.example.com', process.env.PANMAIL_API_KEY!);
 * const result = await client.send({
 *   providerId: '0f8b...',
 *   from: 'noreply@example.com',
 *   to: ['someone@example.org'],
 *   subject: 'Your receipt',
 *   html: '<p>Thanks for your order.</p>',
 * });
 * ```
 *
 * send() resolves once the gateway has written the message to its outbox, not
 * once it has been delivered: result.messageId is what later delivery events
 * and webhooks are keyed by.
 *
 * # Retries
 *
 * This client does not retry a send whose outcome it does not know. Sending is
 * not idempotent and the gateway has no de-duplication key, so a retry after a
 * timeout or a dropped connection is a retry of a message that may already be
 * on its way to the recipient. The one exception is a refusal — the gateway
 * says plainly that it did not accept the message — which is safe to repeat
 * and which `rateLimitRetries` turns on.
 */
export class PanmailClient {
  readonly #endpoint: string;
  readonly #headers: Record<string, string>;
  readonly #timeoutMs: number;
  readonly #rateLimitRetries: number;
  readonly #fetch: typeof globalThis.fetch;

  /**
   * @param baseUrl the gateway's origin — "https://mail.example.com" — not a
   *                path to a procedure
   */
  constructor(baseUrl: string, apiKey: string, options: ClientOptions = {}) {
    validateBaseUrl(baseUrl);
    if (!apiKey) {
      throw new InvalidMessageError('panmail: an api key is required');
    }

    this.#endpoint = withoutTrailingSlashes(baseUrl) + SEND_PROCEDURE;
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#rateLimitRetries = Math.max(0, options.rateLimitRetries ?? 0);
    this.#fetch = options.fetch ?? globalThis.fetch;

    validateHeaders(options.headers ?? {});

    const headers: Record<string, string> = {};
    for (const [name, value] of Object.entries(options.headers ?? {})) {
      // The API key header is not settable this way: it is what the apiKey
      // argument is for, and a second source for it would only make a mismatch
      // possible.
      if (name.toLowerCase() === API_KEY_HEADER.toLowerCase()) continue;
      headers[name] = value;
    }
    headers['Content-Type'] = 'application/json';
    headers[API_KEY_HEADER] = apiKey;
    this.#headers = headers;
  }

  /**
   * Queues a message and resolves once the gateway has it on disk.
   *
   * Resolving means the gateway accepted responsibility for delivering the
   * message, not that it has been delivered — that is reported afterwards
   * through delivery events and webhooks, keyed by result.messageId.
   *
   * Rejects with InvalidMessageError, RateLimitedError, BacklogFullError,
   * AuthError, ApiError or TransportError.
   */
  async send(message: Message): Promise<Result> {
    const body = JSON.stringify(toWire(message));

    for (let attempt = 0; ; attempt++) {
      try {
        return await this.#post(body);
      } catch (error) {
        if (
          !(error instanceof RateLimitedError) ||
          attempt >= this.#rateLimitRetries ||
          error.retryAfter <= 0
        ) {
          throw error;
        }
        await sleep(error.retryAfter * 1000);
      }
    }
  }

  async #post(body: string): Promise<Result> {
    const abort = new AbortController();
    // The timer covers the whole exchange, not just the headers. fetch()
    // settles as soon as the response line arrives, so a gateway that sends
    // headers and then stalls would otherwise hold the caller open with no
    // bound at all: the signal stays armed until the payload is in hand.
    const timer = setTimeout(() => abort.abort(), this.#timeoutMs);

    try {
      let response: Response;
      try {
        response = await this.#fetch(this.#endpoint, {
          method: 'POST',
          headers: this.#headers,
          body,
          signal: abort.signal,
          // X-API-Key is a header fetch has never heard of, so it is not among
          // the credentials undici drops when a redirect crosses to another
          // host — unlike Authorization, which it does drop. Following one
          // would hand the tenant's api key to whatever host the Location
          // named. A Connect procedure has no reason to redirect, so the safe
          // reading of one is that something is wrong.
          redirect: 'error',
        });
      } catch (cause) {
        // Deliberately not classified and never retried: a transport error is
        // the one outcome where the client does not know whether the gateway
        // took the message.
        throw new TransportError(`panmail: the send did not complete: ${String(cause)}`, cause);
      }

      const payload = await readBounded(response);

      if (!response.ok) {
        throw classify(payload, response.status, response.headers);
      }

      let decoded: { messageId?: string; status?: string };
      try {
        decoded = JSON.parse(payload) as { messageId?: string; status?: string };
      } catch (cause) {
        throw new TransportError(`panmail: the gateway's response was not json: ${payload}`, cause);
      }

      return {
        messageId: decoded.messageId ?? '',
        status: (decoded.status ?? '') as Status,
      };
    } finally {
      clearTimeout(timer);
    }
  }
}

function validateBaseUrl(baseUrl: string): void {
  if (!baseUrl) {
    throw new InvalidMessageError('panmail: a base url is required');
  }

  let parsed: URL;
  try {
    parsed = new URL(baseUrl);
  } catch {
    // A missing scheme is the usual mistake — "mail.example.com" does not
    // parse as a URL at all, and left alone the failure surfaces much later as
    // an unreadable transport error.
    throw new InvalidMessageError(`panmail: base url is not a url: ${baseUrl}`);
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new InvalidMessageError(
      `panmail: base url needs an http or https scheme, got "${baseUrl}"`,
    );
  }
  if (!parsed.host) {
    throw new InvalidMessageError(`panmail: base url has no host: "${baseUrl}"`);
  }
  // The api key is a tenant-wide sending credential and http puts it on the
  // wire in the clear. Loopback is exempt because a gateway on localhost is how
  // the thing is developed against, and there is no network to listen on.
  if (parsed.protocol === 'http:' && !isLoopback(parsed.hostname)) {
    throw new InvalidMessageError(
      `panmail: base url "${baseUrl}" uses http, which would send the api key in cleartext; ` +
        'use https, or http only against a loopback host',
    );
  }
  // Userinfo is deprecated in RFC 3986 for the reason it matters here. undici
  // refuses such a url anyway, but it does so by throwing a TypeError that
  // quotes the whole url — password included — which this client would then
  // put in the message of a TransportError, which is a thing that gets logged.
  // The api key is the credential; a proxy wanting basic auth gets a header.
  if (parsed.username !== '' || parsed.password !== '') {
    throw new InvalidMessageError(
      'panmail: base url must not carry credentials; the api key authenticates the send, ' +
        'and a password in the url ends up in logs',
    );
  }
}

/**
 * Reads the response body, refusing one larger than MAX_RESPONSE_BYTES rather
 * than buffering it whole. Bounded in time by the caller's abort signal and in
 * size by the limit here — response.text() is bounded by neither.
 */
async function readBounded(response: Response): Promise<string> {
  const stream = response.body;
  // Nothing to meter: a 204, or a Response built without a stream.
  if (!stream) return response.text();

  const reader = stream.getReader();
  const decoder = new TextDecoder();
  const chunks: string[] = [];
  let size = 0;

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done || value === undefined) break;

      size += value.byteLength;
      if (size > MAX_RESPONSE_BYTES) {
        await reader.cancel();
        throw new TransportError(
          `panmail: the gateway's response exceeded ${MAX_RESPONSE_BYTES} bytes`,
        );
      }
      chunks.push(decoder.decode(value, { stream: true }));
    }
  } catch (cause) {
    if (cause instanceof TransportError) throw cause;
    // An abort mid-body lands here, which is how the timeout is reported.
    throw new TransportError(`panmail: reading the response failed: ${String(cause)}`, cause);
  }

  chunks.push(decoder.decode());
  return chunks.join('');
}

/**
 * Refuses a header that would not survive being written to the wire as
 * written.
 *
 * fetch would refuse it too, but only at send time and as a TransportError —
 * the one failure this client reports as an unknown outcome, when in truth
 * nothing was sent at all. A header that cannot be sent is knowable when the
 * client is built.
 */
function validateHeaders(headers: Record<string, string>): void {
  // RFC 9110's token, which is what a header name is.
  const NAME = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;
  // Tab is allowed; nothing else below space is, and CR and LF end the line.
  // eslint-disable-next-line no-control-regex
  const CONTROL = /[\u0000-\u0008\u000a-\u001f\u007f]/;

  for (const [name, value] of Object.entries(headers)) {
    if (!NAME.test(name)) {
      throw new InvalidMessageError(`panmail: "${name}" is not a header name`);
    }
    if (CONTROL.test(value)) {
      throw new InvalidMessageError(
        `panmail: the value of header "${name}" contains a control character; ` +
          'a carriage return or newline there would add a header of its own',
      );
    }
  }
}

/**
 * Is this host this machine — the one place plaintext http carries the api key
 * no further than the process next to it?
 */
function isLoopback(hostname: string): boolean {
  // URL keeps the brackets on an IPv6 literal.
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '');

  if (host === 'localhost' || host === '::1') return true;

  // The whole of 127.0.0.0/8, not just 127.0.0.1.
  const v4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host);
  return v4 !== null && v4.slice(1).every((n) => Number(n) <= 255) && v4[1] === '127';
}

/**
 * Trims trailing slashes without a regular expression.
 *
 * `/\/+$/` is anchored at the end but not the start, so on a url that is
 * mostly slashes the engine retries the match from every slash in turn and the
 * cost is quadratic: 80,000 of them took 2.3 seconds, against nothing here.
 * The base url comes from whoever embeds this library rather than from a
 * request, which makes it unlikely rather than impossible — and a loop is no
 * harder to read than the regex was.
 */
function withoutTrailingSlashes(baseUrl: string): string {
  let end = baseUrl.length;
  while (end > 0 && baseUrl[end - 1] === '/') end--;
  return baseUrl.slice(0, end);
}
