import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';

import { describe, expect, test } from 'bun:test';

import {
  DEFAULT_WEBHOOK_TOLERANCE_MS,
  WebhookError,
  WebhookHeaders,
  verifyWebhook,
} from './webhook.js';

const SECRET = 'whsec_test';

/**
 * Written out rather than imported from the module under test, so this checks
 * against panmail's own scheme rather than against the SDK agreeing with
 * itself.
 */
const signLikeTheGateway = (secret: string, timestamp: number, body: string) => {
  const mac = createHmac('sha256', secret);
  mac.update(`${timestamp}.`);
  mac.update(Buffer.from(body, 'utf8'));
  return `sha256=${mac.digest('hex')}`;
};

const bodyOf = (over: Record<string, unknown> = {}) =>
  JSON.stringify({
    event: 'mail.bounced',
    tenant_id: '3f1c2b7a-0000-4000-8000-000000000001',
    timestamp: Math.floor(Date.now() / 1000),
    data: { messageId: 'msg_01', reason: 'mailbox full' },
    ...over,
  });

const headersFor = (body: string, secret = SECRET, sentAt = Date.now()) => {
  const seconds = Math.floor(sentAt / 1000);
  return {
    [WebhookHeaders.Event]: 'mail.bounced',
    [WebhookHeaders.Delivery]: 'delivery-1',
    [WebhookHeaders.Timestamp]: String(seconds),
    [WebhookHeaders.Signature]: signLikeTheGateway(secret, seconds, body),
  };
};

describe('verifyWebhook', () => {
  test('accepts what the gateway sent', () => {
    const body = bodyOf();

    const event = verifyWebhook(SECRET, headersFor(body), body);

    expect(event.event).toBe('mail.bounced');
    expect(event.tenantId).toBe('3f1c2b7a-0000-4000-8000-000000000001');
    expect(event.deliveryId).toBe('delivery-1');
    expect((event.data as { reason: string }).reason).toBe('mailbox full');
  });

  // The gateway delivers a subscription with no secret unsigned rather than
  // not delivering it, so an unsigned request is a real thing that arrives at
  // a real endpoint. Accepting one would make verification decorative.
  test('refuses an unsigned delivery', () => {
    const body = bodyOf();
    expect(() =>
      verifyWebhook(SECRET, { [WebhookHeaders.Event]: 'mail.sent' }, body),
    ).toThrow(/carried no signature/);
  });

  test('refuses a tampered body', () => {
    const body = bodyOf();
    const headers = headersFor(body);
    const tampered = body.replace('mailbox full', 'mailbox FULL');

    expect(() => verifyWebhook(SECRET, headers, tampered)).toThrow(WebhookError);
  });

  test('refuses the wrong secret', () => {
    const body = bodyOf();
    expect(() => verifyWebhook('whsec_someone_elses', headersFor(body), body)).toThrow(
      /does not match/,
    );
  });

  // A signature of a different length must not throw out of timingSafeEqual —
  // it is a refusal like any other.
  test('refuses a signature of the wrong length without crashing', () => {
    const body = bodyOf();
    const headers = { ...headersFor(body), [WebhookHeaders.Signature]: 'sha256=deadbeef' };

    expect(() => verifyWebhook(SECRET, headers, body)).toThrow(WebhookError);
  });

  // The window is what stops a captured request being replayed tomorrow, so it
  // is checked in both directions.
  test.each([
    ['long ago', -30 * 60 * 1000],
    ['the future', 30 * 60 * 1000],
  ])('refuses a timestamp %s', (_label, offset) => {
    const body = bodyOf();
    const headers = headersFor(body, SECRET, Date.now() + (offset as number));

    expect(() => verifyWebhook(SECRET, headers, body)).toThrow(/tolerance/);
    // The same delivery inside a wider window is fine, which shows the refusal
    // was the clock and not the signature.
    expect(() => verifyWebhook(SECRET, headers, body, { toleranceMs: 60 * 60 * 1000 })).not.toThrow();
  });

  test('refuses a timestamp that is not a number', () => {
    const body = bodyOf();
    const headers = { ...headersFor(body), [WebhookHeaders.Timestamp]: 'yesterday' };

    expect(() => verifyWebhook(SECRET, headers, body)).toThrow(/not a number/);
  });

  test('needs a secret', () => {
    const body = bodyOf();
    expect(() => verifyWebhook('', headersFor(body), body)).toThrow(/no signing secret/);
  });

  // Signed by the right secret, but not the envelope this version knows.
  test('separates a signed body it cannot read from a bad signature', () => {
    const body = '["signed", "but not an envelope"]';
    expect(() => verifyWebhook(SECRET, headersFor(body), body)).toThrow(/not a webhook envelope/);
  });

  test('falls back to the event header when the envelope has no event', () => {
    const body = bodyOf({ event: undefined });
    expect(verifyWebhook(SECRET, headersFor(body), body).event).toBe('mail.bounced');
  });

  test('a non-positive tolerance falls back to the default', () => {
    const body = bodyOf();
    expect(() => verifyWebhook(SECRET, headersFor(body), body, { toleranceMs: 0 })).not.toThrow();
    expect(DEFAULT_WEBHOOK_TOLERANCE_MS).toBe(300_000);
  });

  // The shapes a framework actually hands over.
  test('reads headers from a Headers object and from lowercased keys', () => {
    const body = bodyOf();
    const plain = headersFor(body);

    expect(verifyWebhook(SECRET, new Headers(plain), body).event).toBe('mail.bounced');

    const lowercased = Object.fromEntries(
      Object.entries(plain).map(([k, v]) => [k.toLowerCase(), v]),
    );
    expect(verifyWebhook(SECRET, lowercased, body).event).toBe('mail.bounced');
  });

  // The signature covers the bytes that arrived, not the value they decode to.
  test('refuses a body that was reformatted', () => {
    const arrived = JSON.stringify(JSON.parse(bodyOf()), null, 2);
    const headers = headersFor(arrived);

    expect(() => verifyWebhook(SECRET, headers, arrived)).not.toThrow();
    expect(() => verifyWebhook(SECRET, headers, JSON.stringify(JSON.parse(arrived)))).toThrow(
      WebhookError,
    );
  });

  // Bytes, not a string: a Buffer is what a raw-body middleware hands over.
  test('accepts the raw bytes a middleware provides', () => {
    const body = bodyOf();
    const event = verifyWebhook(SECRET, headersFor(body), Buffer.from(body, 'utf8'));
    expect(event.event).toBe('mail.bounced');
  });
});

// The vectors in testdata are computed independently — in Python by
// scripts/sync-webhook-vectors.py — and were checked against the gateway's own
// sign(). Matching them is agreeing with something other than ourselves, and it
// is what stops the three clients drifting apart on the one calculation where
// disagreeing means silently rejecting real deliveries.
describe('the shared signature vectors', () => {
  const fixture = JSON.parse(
    readFileSync(new URL('../../testdata/webhook-signatures.json', import.meta.url), 'utf8'),
  ) as { vectors: Array<{ name: string; secret: string; timestamp: number; body: string; signature: string }> };

  test('there are some', () => {
    expect(fixture.vectors.length).toBeGreaterThan(0);
  });

  for (const v of fixture.vectors) {
    test(`${v.name} signs as the gateway does`, () => {
      const headers = {
        [WebhookHeaders.Timestamp]: String(v.timestamp),
        [WebhookHeaders.Signature]: v.signature,
      };
      // The vectors use fixed timestamps, so the window is widened to leave
      // only the signature under test.
      const verify = () =>
        verifyWebhook(v.secret, headers, v.body, { toleranceMs: 100 * 365 * 24 * 3600 * 1000 });

      // A body that is valid JSON but not an envelope still proves the
      // signature matched: that refusal happens after the HMAC check.
      try {
        expect(verify().timestamp.getTime()).toBe(v.timestamp * 1000);
      } catch (error) {
        if (!(error instanceof WebhookError)) throw error;
        expect(error.message).toMatch(/not a webhook envelope/);
      }
    });
  }
});
