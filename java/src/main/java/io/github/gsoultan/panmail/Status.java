package io.github.gsoultan.panmail;

/**
 * The state a message is in.
 *
 * <p>The values are the gateway's own enum names, carried verbatim: the wire
 * format is protobuf JSON, which encodes an enum as its name rather than its
 * number. Comparing against these is comparing against exactly what the
 * gateway sent.
 */
public final class Status {

    /**
     * What a successful send reports: the gateway has the message on disk and
     * will deliver it. Delivery itself is reported later, through events and
     * webhooks.
     */
    public static final String PENDING = "EMAIL_EVENT_TYPE_PENDING";

    /**
     * The rest are what the gateway reports afterwards, on the event stream
     * and through webhooks, keyed by {@link Result#messageId()}. A send never
     * returns them.
     *
     * <p>BOUNCED is the general refusal; HARD_BOUNCE and SOFT_BOUNCE are the
     * gateway saying which kind, when the provider told it. A hard bounce is
     * an address that will never accept mail and should be suppressed; a soft
     * one is a mailbox that was full or a server that was down.
     */
    public static final String SENT = "EMAIL_EVENT_TYPE_SENT";
    public static final String DELIVERED = "EMAIL_EVENT_TYPE_DELIVERED";
    public static final String OPENED = "EMAIL_EVENT_TYPE_OPENED";
    public static final String CLICKED = "EMAIL_EVENT_TYPE_CLICKED";
    public static final String BOUNCED = "EMAIL_EVENT_TYPE_BOUNCED";
    public static final String HARD_BOUNCE = "EMAIL_EVENT_TYPE_HARD_BOUNCE";
    public static final String SOFT_BOUNCE = "EMAIL_EVENT_TYPE_SOFT_BOUNCE";
    public static final String SPAM_REPORT = "EMAIL_EVENT_TYPE_SPAM_REPORT";
    public static final String UNSUBSCRIBED = "EMAIL_EVENT_TYPE_UNSUBSCRIBED";
    public static final String DROPPED = "EMAIL_EVENT_TYPE_DROPPED";
    public static final String REJECTED = "EMAIL_EVENT_TYPE_REJECTED";
    public static final String DEFERRED = "EMAIL_EVENT_TYPE_DEFERRED";
    public static final String COMPLAINED = "EMAIL_EVENT_TYPE_COMPLAINED";

    /**
     * protobuf's zero value, not a state a message is ever in. Here because
     * the gateway's enum has it, and a client that quietly omitted a value
     * would be the start of the two drifting.
     */
    public static final String UNSPECIFIED = "EMAIL_EVENT_TYPE_UNSPECIFIED";

    private Status() {
    }
}
