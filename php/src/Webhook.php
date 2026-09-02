<?php

declare(strict_types=1);

namespace Panmail;

use Panmail\Exception\WebhookException;

/**
 * Verifies that a webhook delivery came from the gateway.
 *
 * The gateway signs the timestamp and the body together under HMAC-SHA256:
 * signing only the body would mean a notification captured once could be
 * replayed forever against the same signature.
 *
 *     $event = Webhook::verify($secret, getallheaders(), file_get_contents('php://input'));
 *
 * The body must be the bytes as they arrived. Re-serialising a decoded payload
 * will not verify — the signature covers what was sent, down to key order and
 * whitespace — so read the raw input first and decode afterwards.
 */
final class Webhook
{
    /**
     * Carries the HMAC, prefixed with the algorithm so a future scheme can be
     * introduced without a receiver having to guess which one it is looking at.
     */
    public const SIGNATURE_HEADER = 'X-Panmail-Signature';

    /**
     * The second half of what is signed. Without it a notification captured
     * once could be replayed forever against the same signature.
     */
    public const TIMESTAMP_HEADER = 'X-Panmail-Timestamp';

    /**
     * Lets a receiver route without parsing the body. The value is a dotted
     * name — "mail.sent", "mail.bounced" — and not one of the Status
     * constants, which name delivery states rather than events.
     */
    public const EVENT_HEADER = 'X-Panmail-Event';

    /**
     * Stable across retries of the same notification, which is what lets a
     * receiver make its own handling idempotent. The gateway does retry, so a
     * handler that is not idempotent will act twice.
     */
    public const DELIVERY_HEADER = 'X-Panmail-Delivery';

    /**
     * How far a delivery's timestamp may be from now, in seconds.
     *
     * It exists because clocks disagree, not because old notifications are
     * acceptable: the window is what stops a captured request being replayed
     * tomorrow, so wider is weaker.
     */
    public const DEFAULT_TOLERANCE = 300;

    /**
     * @param array<string, string|list<string>> $headers as any framework hands them over
     *
     * @throws WebhookException when the request is not known to have come from
     *                          the gateway. There is nothing to salvage from
     *                          one: do not act on the payload, and do not echo
     *                          the reason back to the caller.
     */
    public static function verify(
        string $secret,
        array $headers,
        string $body,
        int $tolerance = self::DEFAULT_TOLERANCE,
    ): WebhookEvent {
        if ($secret === '') {
            throw new WebhookException('no signing secret was supplied');
        }
        if ($tolerance <= 0) {
            $tolerance = self::DEFAULT_TOLERANCE;
        }

        $signature = self::header($headers, self::SIGNATURE_HEADER);
        $rawTimestamp = self::header($headers, self::TIMESTAMP_HEADER);
        if ($signature === '' || $rawTimestamp === '') {
            // The gateway delivers a subscription with no secret unsigned
            // rather than not delivering it, so this is a real request at a
            // real endpoint. Accepting it would make verification decorative.
            throw new WebhookException('the webhook carried no signature');
        }

        $trimmed = trim($rawTimestamp);
        if (preg_match('/^-?\d+$/', $trimmed) !== 1) {
            throw new WebhookException("the timestamp \"$rawTimestamp\" is not a number");
        }
        $seconds = (int) $trimmed;

        // Both directions: a timestamp far in the future is as much a sign of
        // something wrong as one far in the past, and only checking the past
        // would let a forged future timestamp keep a capture valid forever.
        $drift = time() - $seconds;
        if ($drift > $tolerance || $drift < -$tolerance) {
            throw new WebhookException(
                "the timestamp is {$drift}s away from now, outside the {$tolerance}s tolerance"
            );
        }

        // hash_equals rather than ===, so the comparison does not return early
        // on the first differing byte and leak how much of a guess was right.
        if (!hash_equals(self::sign($secret, $seconds, $body), $signature)) {
            throw new WebhookException('the signature does not match the body');
        }

        /** @var mixed $envelope */
        $envelope = json_decode($body, true);
        if (!is_array($envelope) || array_is_list($envelope)) {
            // The signature already matched, so this is the gateway sending
            // something this version does not understand, not an attack.
            throw new WebhookException('the body is signed but not a webhook envelope');
        }

        $event = is_scalar($envelope['event'] ?? null) ? (string) $envelope['event'] : '';
        if ($event === '') {
            $event = self::header($headers, self::EVENT_HEADER);
        }

        return new WebhookEvent(
            event: $event,
            tenantId: is_scalar($envelope['tenant_id'] ?? null) ? (string) $envelope['tenant_id'] : '',
            timestamp: $seconds,
            deliveryId: self::header($headers, self::DELIVERY_HEADER),
            data: $envelope['data'] ?? null,
        );
    }

    /**
     * The gateway's scheme: the timestamp, a dot, then the body, under
     * HMAC-SHA256, hex encoded and prefixed with the algorithm.
     */
    private static function sign(string $secret, int $timestamp, string $body): string
    {
        return 'sha256=' . hash_hmac('sha256', $timestamp . '.' . $body, $secret);
    }

    /**
     * Header names are case insensitive, and PHP hands them over in whichever
     * case the front end felt like.
     *
     * @param array<string, string|list<string>> $headers
     */
    private static function header(array $headers, string $name): string
    {
        foreach ($headers as $key => $value) {
            if (strcasecmp((string) $key, $name) === 0) {
                $first = is_array($value) ? ($value[0] ?? '') : $value;

                return (string) $first;
            }
        }

        return '';
    }
}
