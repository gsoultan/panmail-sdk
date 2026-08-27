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

    public const SENT = 'EMAIL_EVENT_TYPE_SENT';
    public const DELIVERED = 'EMAIL_EVENT_TYPE_DELIVERED';
    public const DROPPED = 'EMAIL_EVENT_TYPE_DROPPED';

    private function __construct()
    {
    }
}
