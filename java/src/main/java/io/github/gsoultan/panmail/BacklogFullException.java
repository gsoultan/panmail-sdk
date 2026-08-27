package io.github.gsoultan.panmail;

/**
 * The tenant already has more queued than its own send rate can drain in the
 * near future. The message was not accepted.
 *
 * <p>Unlike a rate refusal this carries no delay, because there is none to
 * give: a queue clearing is not something a caller can schedule against.
 * Retrying on a timer will not help, and retrying immediately makes the wait
 * longer for everything already queued. Slow down, or stop.
 */
public class BacklogFullException extends ApiException {

    public BacklogFullException(String message, String code, int status) {
        super(message, code, status);
    }
}
