<?php

declare(strict_types=1);

namespace Panmail;

/**
 * A configured provider: as much of one as a sender needs.
 *
 * That is deliberately not all of it. The gateway also returns the transport
 * configuration — host, username, region, the SES access key id — with every
 * credential already cleared. None of it is needed to send, and leaving it out
 * means a lapse in that clearing has nowhere in this client to land: a
 * Provider holds nothing that needs keeping out of a log.
 */
final class Provider
{
    /**
     * @param list<string> $allowedDomains the From domains the provider will
     *                                     send as, each compared whole and
     *                                     ignoring case: example.com does not
     *                                     admit mail.example.com. Empty means the
     *                                     operator has not restricted it.
     */
    public function __construct(
        /** What a Message's providerId takes. */
        public readonly string $id,

        /** The name shown on the Email Providers page. */
        public readonly string $name,

        /** A ProviderType constant, or the string the gateway sent for a kind this client does not know. */
        public readonly string $type,

        public readonly array $allowedDomains,
    ) {
    }
}
