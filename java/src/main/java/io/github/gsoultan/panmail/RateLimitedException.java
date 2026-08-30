package io.github.gsoultan.panmail;

import java.time.Duration;

/**
 * The tenant is sending faster than its configured rate allows.
 *
 * <p>The message was not accepted, so sending it again after
 * {@link #retryAfter()} is safe.
 */
public class RateLimitedException extends ApiException {

    private static final long serialVersionUID = 1L;

    private final Duration retryAfter;

    public RateLimitedException(String message, Duration retryAfter, String code, int status) {
        super(message, code, status);
        this.retryAfter = retryAfter;
    }

    /** How long to wait, or {@link Duration#ZERO} when the gateway quoted no usable delay. */
    public Duration retryAfter() {
        return retryAfter;
    }
}
