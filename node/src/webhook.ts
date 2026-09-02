import { createHmac, timingSafeEqual } from 'node:crypto';

/**
 * Headers the gateway sets on a webhook delivery.
 *
 * The signature is prefixed with its algorithm so a future scheme can be
 * introduced without a receiver having to guess which one it is looking at.
 * The timestamp is the second half of what is signed — without it, a
 * notification captured once could be replayed forever against the same
 * signature.
 *
 * The event name is a dotted string — "mail.sent", "mail.bounced" — and not
 * one of the `Status` constants, which name delivery states rather than
 * events. The delivery id is stable across retries, and the gateway does
 * retry, so a handler that is not idempotent will act twice.
 */
export const WebhookHeaders = {
  Signature: 'X-Panmail-Signature',
  Timestamp: 'X-Panmail-Timestamp',
  Event: 'X-Panmail-Event',
  Delivery: 'X-Panmail-Delivery',
} as const;

/**
 * How far a delivery's timestamp may be from now, in milliseconds.
 *
 * It exists because clocks disagree, not because old notifications are
 * acceptable: the window is what stops a captured request being replayed
 * tomorrow, so wider is weaker.
 */
export const DEFAULT_WEBHOOK_TOLERANCE_MS = 5 * 60 * 1000;

/**
 * A delivery that could not be trusted.
 *
 * One class rather than several because there is one thing to do about it:
 * refuse the request. `reason` says which check failed, for the log.
 */
export class WebhookError extends Error {
  constructor(readonly reason: string) {
    super(`panmail: the webhook could not be verified: ${reason}`);
    this.name = 'WebhookError';
  }
}

/** A verified delivery. */
export interface WebhookEvent {
  /** The dotted name, e.g. "mail.bounced". */
  event: string;

  /**
   * The tenant the event belongs to. Worth checking against the one you
   * expected, since a single endpoint can serve several.
   */
  tenantId: string;

  /** When the gateway sent it. */
  timestamp: Date;

  /** Stable across retries. Store it and ignore a repeat. */
  deliveryId: string;

  /**
   * The event-specific payload, left unmodelled because its shape depends on
   * `event` and this package does not describe every one of them.
   */
  data: unknown;
}

export interface VerifyWebhookOptions {
  /** Defaults to DEFAULT_WEBHOOK_TOLERANCE_MS. Narrower is stronger. */
  toleranceMs?: number;
}

/** Headers as a framework hands them over, in any of the usual shapes. */
export type WebhookHeaderSource =
  | Headers
  | Record<string, string | string[] | undefined>
  | { get(name: string): string | null };

function headerValue(source: WebhookHeaderSource, name: string): string {
  if (typeof (source as { get?: unknown }).get === 'function') {
    return (source as { get(n: string): string | null }).get(name) ?? '';
  }
  // Node's IncomingMessage lowercases, and a plain object might not, so both
  // spellings are tried rather than assuming the caller normalised anything.
  const record = source as Record<string, string | string[] | undefined>;
  const raw = record[name] ?? record[name.toLowerCase()];
  if (Array.isArray(raw)) return raw[0] ?? '';
  return raw ?? '';
}

/**
 * The gateway's scheme: the timestamp, a dot, then the body, under
 * HMAC-SHA256, hex encoded and prefixed with the algorithm.
 */
function sign(secret: string, timestamp: number, body: Uint8Array): string {
  const mac = createHmac('sha256', secret);
  mac.update(`${timestamp}.`);
  mac.update(body);
  return `sha256=${mac.digest('hex')}`;
}

/**
 * Checks that a delivery came from the gateway, and returns it.
 *
 * `body` must be the bytes as they arrived. Re-serialising a decoded payload
 * will not verify: the signature covers what was sent, down to key order and
 * whitespace, so read the raw body first and parse afterwards. Express with
 * `express.json()` has already thrown the raw bytes away — use
 * `express.raw({ type: 'application/json' })` on this route.
 *
 * ```ts
 * const event = verifyWebhook(secret, req.headers, req.body);
 * ```
 *
 * Throws WebhookError when the request is not known to have come from the
 * gateway. There is nothing to salvage from one: do not act on the payload,
 * and do not echo the reason back to the caller.
 */
export function verifyWebhook(
  secret: string,
  headers: WebhookHeaderSource,
  body: Uint8Array | string,
  options: VerifyWebhookOptions = {},
): WebhookEvent {
  if (!secret) {
    throw new WebhookError('no signing secret was supplied');
  }

  const tolerance =
    options.toleranceMs !== undefined && options.toleranceMs > 0
      ? options.toleranceMs
      : DEFAULT_WEBHOOK_TOLERANCE_MS;

  const raw = typeof body === 'string' ? new TextEncoder().encode(body) : body;

  const signature = headerValue(headers, WebhookHeaders.Signature);
  const rawTimestamp = headerValue(headers, WebhookHeaders.Timestamp);
  if (!signature || !rawTimestamp) {
    // The gateway delivers a subscription with no secret unsigned rather than
    // not delivering it, so this is a real request at a real endpoint.
    // Accepting it would make verification decorative.
    throw new WebhookError('the webhook carried no signature');
  }

  if (!/^-?\d+$/.test(rawTimestamp.trim())) {
    throw new WebhookError(`the timestamp "${rawTimestamp}" is not a number`);
  }
  const seconds = Number(rawTimestamp.trim());
  const sentAt = seconds * 1000;

  // Both directions: a timestamp far in the future is as much a sign of
  // something wrong as one far in the past, and only checking the past would
  // let a forged future timestamp keep a capture valid indefinitely.
  const drift = Date.now() - sentAt;
  if (drift > tolerance || drift < -tolerance) {
    throw new WebhookError(
      `the timestamp is ${Math.round(drift / 1000)}s away from now, outside the ` +
        `${Math.round(tolerance / 1000)}s tolerance`,
    );
  }

  const expected = Buffer.from(sign(secret, seconds, raw));
  const actual = Buffer.from(signature);
  // Length is checked first because timingSafeEqual throws on a mismatch, and
  // the lengths are not the secret — the bytes are.
  if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) {
    throw new WebhookError('the signature does not match the body');
  }

  let envelope: { event?: string; tenant_id?: string; timestamp?: number; data?: unknown };
  try {
    envelope = JSON.parse(Buffer.from(raw).toString('utf8')) as typeof envelope;
  } catch (cause) {
    // The signature already matched, so this is the gateway sending something
    // this version does not understand rather than an attack.
    throw new WebhookError(`the body is signed but not a webhook envelope: ${String(cause)}`);
  }
  if (envelope === null || typeof envelope !== 'object' || Array.isArray(envelope)) {
    throw new WebhookError('the body is signed but not a webhook envelope');
  }

  return {
    event: envelope.event || headerValue(headers, WebhookHeaders.Event),
    tenantId: envelope.tenant_id ?? '',
    timestamp: new Date(sentAt),
    deliveryId: headerValue(headers, WebhookHeaders.Delivery),
    data: envelope.data,
  };
}
