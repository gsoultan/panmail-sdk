<?php

declare(strict_types=1);

namespace Panmail;

/**
 * Why a webhook fired.
 *
 * The values are the gateway's own WebhookTriggerEvent enum names, carried
 * verbatim: it dispatches with event.String(), so what arrives in the
 * X-Panmail-Event header and the envelope's "event" field is the enum name and
 * nothing else.
 *
 * Not to be confused with Status, which names the state a message is in and is
 * what a send returns. They are different enums in the gateway and neither is a
 * superset of the other.
 */
final class TriggerEvent
{
    /**
     * MAIL_SENT through MAIL_BOUNCED are the delivery pipeline reporting what a
     * provider did.
     */
    public const MAIL_SENT = 'WEBHOOK_TRIGGER_EVENT_MAIL_SENT';
    public const MAIL_DELIVERED = 'WEBHOOK_TRIGGER_EVENT_MAIL_DELIVERED';
    public const MAIL_OPENED = 'WEBHOOK_TRIGGER_EVENT_MAIL_OPENED';
    public const MAIL_CLICKED = 'WEBHOOK_TRIGGER_EVENT_MAIL_CLICKED';
    public const MAIL_BOUNCED = 'WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED';

    /**
     * A provider refusing a send. Not MAIL_QUARANTINE_REJECTED, which is a
     * person refusing a held message — a subscriber acting on "rejected" needs
     * to know which one it received.
     */
    public const MAIL_REJECTED = 'WEBHOOK_TRIGGER_EVENT_MAIL_REJECTED';

    public const MAIL_INBOUND = 'WEBHOOK_TRIGGER_EVENT_MAIL_INBOUND';

    /**
     * A filter rule quarantined a message for review. Worth subscribing to:
     * without it a hold is silent, and a message nobody reviewed expires unseen
     * — worse than one that was refused, because nobody ever decided it.
     */
    public const MAIL_HELD = 'WEBHOOK_TRIGGER_EVENT_MAIL_HELD';

    /** The three ways a hold ends. */
    public const MAIL_RELEASED = 'WEBHOOK_TRIGGER_EVENT_MAIL_RELEASED';
    public const MAIL_QUARANTINE_REJECTED = 'WEBHOOK_TRIGGER_EVENT_MAIL_QUARANTINE_REJECTED';

    /**
     * A held message reached its retention deadline with nobody having decided.
     * This is the one to alert on: it does not say a message was refused, it
     * says a queue went unwatched.
     */
    public const MAIL_EXPIRED = 'WEBHOOK_TRIGGER_EVENT_MAIL_EXPIRED';

    /**
     * protobuf's zero value, not a reason a webhook ever fires. Here because
     * the gateway's enum has it, and a client that quietly omitted a value
     * would be the start of the two drifting.
     */
    public const UNSPECIFIED = 'WEBHOOK_TRIGGER_EVENT_UNSPECIFIED';

    private function __construct()
    {
    }
}
