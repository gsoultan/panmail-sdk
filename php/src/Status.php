<?php

declare(strict_types=1);

namespace Panmail;

/**
 * The state a message is in.
 *
 * The values are the gateway's own enum names, carried verbatim: the wire
 * format is protobuf JSON, which encodes an enum as its name rather than its
 * number. Comparing against these is comparing against exactly what the
 * gateway sent.
 */
final class Status
{
    /**
     * What a successful send reports: the gateway has the message on disk and
     * will deliver it. Delivery itself is reported later, through events and
     * webhooks.
     */
    public const PENDING = 'EMAIL_EVENT_TYPE_PENDING';

    /**
     * The rest are what the gateway reports afterwards, on the event stream
     * and through webhooks, keyed by $result->messageId. A send never returns
     * them.
     *
     * BOUNCED is the general refusal; HARD_BOUNCE and SOFT_BOUNCE are the
     * gateway saying which kind, when the provider told it. A hard bounce is
     * an address that will never accept mail and should be suppressed; a soft
     * one is a mailbox that was full or a server that was down.
     */
    public const SENT = 'EMAIL_EVENT_TYPE_SENT';
    public const DELIVERED = 'EMAIL_EVENT_TYPE_DELIVERED';
    public const OPENED = 'EMAIL_EVENT_TYPE_OPENED';
    public const CLICKED = 'EMAIL_EVENT_TYPE_CLICKED';
    public const BOUNCED = 'EMAIL_EVENT_TYPE_BOUNCED';
    public const HARD_BOUNCE = 'EMAIL_EVENT_TYPE_HARD_BOUNCE';
    public const SOFT_BOUNCE = 'EMAIL_EVENT_TYPE_SOFT_BOUNCE';
    public const SPAM_REPORT = 'EMAIL_EVENT_TYPE_SPAM_REPORT';
    public const UNSUBSCRIBED = 'EMAIL_EVENT_TYPE_UNSUBSCRIBED';
    public const DROPPED = 'EMAIL_EVENT_TYPE_DROPPED';
    public const REJECTED = 'EMAIL_EVENT_TYPE_REJECTED';
    public const DEFERRED = 'EMAIL_EVENT_TYPE_DEFERRED';
    public const COMPLAINED = 'EMAIL_EVENT_TYPE_COMPLAINED';

    /**
     * protobuf's zero value, not a state a message is ever in. Here because
     * the gateway's enum has it, and a client that quietly omitted a value
     * would be the start of the two drifting.
     */
    public const UNSPECIFIED = 'EMAIL_EVENT_TYPE_UNSPECIFIED';

    private function __construct()
    {
    }
}
