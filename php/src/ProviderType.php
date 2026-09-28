<?php

declare(strict_types=1);

namespace Panmail;

/**
 * The kind of provider: which vendor, or which protocol, mail goes through.
 *
 * Like Status, the values are the gateway's own enum names, carried verbatim.
 * They are constants on a class rather than a PHP enum because the gateway
 * adds kinds — four API vendors arrived at once — and a kind this client has
 * no constant for still comes through as the string the gateway sent, rather
 * than failing the call or being mistaken for another.
 */
final class ProviderType
{
    public const SMTP = 'PROVIDER_TYPE_SMTP';
    public const SENDGRID = 'PROVIDER_TYPE_SENDGRID';
    public const SES = 'PROVIDER_TYPE_SES';
    public const POSTMARK = 'PROVIDER_TYPE_POSTMARK';
    public const MAILGUN = 'PROVIDER_TYPE_MAILGUN';

    /**
     * IMAP and POP3 are mailboxes inbound mail is read from, not something to
     * send through; the gateway passes over them when it picks a provider to
     * send with. They are listed because the gateway lists them, so check the
     * type before offering a provider as a sender.
     */
    public const IMAP = 'PROVIDER_TYPE_IMAP';
    public const POP3 = 'PROVIDER_TYPE_POP3';

    /**
     * protobuf's zero value, not a kind of provider. As a filter it means no
     * filter. It is also what a provider sent with no type reads as, because
     * protobuf JSON leaves a zero value out rather than spelling it.
     */
    public const UNSPECIFIED = 'PROVIDER_TYPE_UNSPECIFIED';

    private function __construct()
    {
    }
}
