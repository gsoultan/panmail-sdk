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

    public static final String SENT = "EMAIL_EVENT_TYPE_SENT";
    public static final String DELIVERED = "EMAIL_EVENT_TYPE_DELIVERED";
    public static final String DROPPED = "EMAIL_EVENT_TYPE_DROPPED";

    private Status() {
    }
}
