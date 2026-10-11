<?php

declare(strict_types=1);

namespace Panmail;

/** A verified webhook delivery with its data decoded. */
final class ParsedWebhook
{
    public function __construct(
        /** The verified envelope, as Webhook::verify() returns it. Its data is still the raw payload. */
        public readonly WebhookEvent $webhook,

        /**
         * The data decoded by the webhook's event. Null for an event this
         * version has no class for — the gateway adds them — which is not an
         * error: the delivery was verified, and $webhook->data has the payload.
         */
        public readonly ?WebhookPayload $payload,
    ) {
    }
}
