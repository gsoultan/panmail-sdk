<?php

declare(strict_types=1);

namespace Panmail;

/** A verified webhook delivery. */
final class WebhookEvent
{
    /**
     * @param string $event      the dotted name, e.g. "mail.bounced"
     * @param string $tenantId   the tenant the event belongs to; worth checking
     *                           against the one you expected, since a single
     *                           endpoint can serve several
     * @param int    $timestamp  unix seconds, when the gateway sent it
     * @param string $deliveryId stable across retries — store it and ignore a repeat
     * @param mixed  $data       the event-specific payload, left undecoded beyond
     *                           JSON because its shape depends on $event
     */
    public function __construct(
        public readonly string $event,
        public readonly string $tenantId,
        public readonly int $timestamp,
        public readonly string $deliveryId,
        public readonly mixed $data,
    ) {
    }
}
