<?php

declare(strict_types=1);

namespace Panmail;

/**
 * A configured provider: as much of one as a sender needs.
 *
 * That is deliberately not all of it. The gateway also returns the transport
 * configuration — host, region, the SES access key id — with every credential
 * already cleared. Of that, only the username is kept, so an account can be
 * told apart from another on the same host; nothing that authenticates as it
 * ever is, which means a lapse in that clearing has nowhere in this client to
 * land.
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

        /**
         * The account an SMTP, IMAP or POP3 provider signs in as. Empty for the
         * API vendors, which authenticate with a key rather than an account.
         * The password is never returned by the gateway and never kept here.
         */
        public readonly string $username = '',
    ) {
    }
}
