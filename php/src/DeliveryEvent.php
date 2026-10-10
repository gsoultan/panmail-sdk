<?php

declare(strict_types=1);

namespace Panmail;

/**
 * One thing that happened to a message on its way to one recipient: queued,
 * sent, delivered, bounced, opened.
 *
 * As much of the gateway's EmailEvent as a sender needs to tell what became of
 * a message. The gateway also returns the provider's name, the subject, the
 * tenant id and a free-form metadata object — the click's URL, a provider's own
 * bounce codes. None of it is kept: the subject is the caller's own, and the
 * metadata is a provider's vocabulary rather than the gateway's, with no shape
 * to promise.
 */
final class DeliveryEvent
{
    public function __construct(
        /** The event's own id. */
        public readonly string $id,

        /** The id send() returned. */
        public readonly string $messageId,

        /**
         * What happened, a Status constant. A value the gateway's enum has no
         * name for comes through as its decimal spelling rather than failing
         * the call.
         */
        public readonly string $type,

        /**
         * The address it happened for. A message to three people has three
         * histories, and they need not agree.
         */
        public readonly string $recipient,

        /** When the gateway recorded it; null if the gateway sent no time. */
        public readonly ?\DateTimeImmutable $timestamp,

        /**
         * The provider that handled the message, as a Message's providerId
         * takes it. Empty for an event recorded before a provider was chosen.
         */
        public readonly string $providerId,

        /**
         * The provider's or the gateway's explanation, for an event that has
         * one: why a bounce bounced, why a send was rejected. Prose, for a
         * person or a log.
         */
        public readonly string $reason,
    ) {
    }
}
