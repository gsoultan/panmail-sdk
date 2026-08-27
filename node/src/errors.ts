/** Base class for everything this client throws. */
export class PanmailError extends Error {
  constructor(message: string) {
    super(message);
    this.name = new.target.name;
  }
}

/**
 * A message this client refused before sending it.
 *
 * Validated locally as well as on the server so that the obvious mistakes cost
 * a clear error rather than a round trip and a rejection phrased in the
 * gateway's terms.
 */
export class InvalidMessageError extends PanmailError {}

/**
 * The send did not complete.
 *
 * This is the one outcome where the client cannot know whether the gateway
 * took the message, which is why it is never retried automatically.
 */
export class TransportError extends PanmailError {
  constructor(
    message: string,
    readonly cause?: unknown,
  ) {
    super(message);
  }
}

/**
 * A refusal this client has no more specific type for.
 *
 * `code` is the Connect error code the gateway sent — "invalid_argument",
 * "internal" and so on — and `status` is the HTTP status it arrived with.
 */
export class ApiError extends PanmailError {
  constructor(
    message: string,
    readonly code: string = '',
    readonly status: number = 0,
  ) {
    super(message);
  }
}

/**
 * The tenant is sending faster than its configured rate allows.
 *
 * The message was not accepted, so sending it again after `retryAfter` seconds
 * is safe.
 */
export class RateLimitedError extends ApiError {
  constructor(
    message: string,
    /** Seconds to wait, or 0 when the gateway quoted no usable delay. */
    readonly retryAfter: number = 0,
    code = '',
    status = 0,
  ) {
    super(message, code, status);
  }
}

/**
 * The tenant already has more queued than its own send rate can drain in the
 * near future. The message was not accepted.
 *
 * Unlike a rate refusal this carries no delay, because there is none to give:
 * a queue clearing is not something a caller can schedule against. Retrying on
 * a timer will not help, and retrying immediately makes the wait longer for
 * everything already queued. Slow down, or stop.
 */
export class BacklogFullError extends ApiError {}

/** The API key was missing, rejected, or lacks the email:send scope. */
export class AuthError extends ApiError {}

/** Connect error codes this client acts on. */
const RESOURCE_EXHAUSTED = 'resource_exhausted';
const UNAUTHENTICATED = 'unauthenticated';
const PERMISSION_DENIED = 'permission_denied';

/**
 * Turns a refusal into something a caller can act on.
 *
 * The gateway answers both of its capacity refusals with resource_exhausted,
 * deliberately: they are the same answer to the client — you are asking for
 * more than you may have. What separates them is Retry-After, which the rate
 * limiter sets and the backlog check does not, because only one of the two has
 * a delay worth quoting. That is the discrimination here, and it is why
 * removing Retry-After from the rate refusal would silently reclassify every
 * rate limit as a full queue.
 */
export function classify(payload: string, status: number, headers: Headers): ApiError {
  let code = '';
  let message = '';

  try {
    const envelope = JSON.parse(payload) as { code?: string; message?: string };
    code = envelope.code ?? '';
    message = envelope.message ?? '';
  } catch {
    // A body that is not the Connect envelope is a proxy's error page. The
    // status alone is still worth classifying.
  }
  if (message === '') message = payload.trim();

  if (code === '') {
    if (status === 429) code = RESOURCE_EXHAUSTED;
    else if (status === 401) code = UNAUTHENTICATED;
    else if (status === 403) code = PERMISSION_DENIED;
  }

  if (code === RESOURCE_EXHAUSTED) {
    const raw = headers.get('Retry-After');
    if (raw === null) {
      return new BacklogFullError(
        `panmail: the tenant's queue is too deep to accept more: ${message}`,
        code,
        status,
      );
    }
    // A header this client cannot read is not a reason to call the refusal
    // something else: it is still a rate limit, just one with no usable delay.
    const seconds = /^\d+$/.test(raw.trim()) ? Number(raw.trim()) : 0;
    return new RateLimitedError(
      `panmail: send rate exceeded, retry after ${seconds}s: ${message}`,
      seconds,
      code,
      status,
    );
  }

  if (code === UNAUTHENTICATED || code === PERMISSION_DENIED) {
    return new AuthError(`panmail: the api key was not accepted: ${message}`, code, status);
  }

  return new ApiError(`panmail: ${code}: ${message}`, code, status);
}
