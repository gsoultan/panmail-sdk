package io.github.gsoultan.panmail;

/**
 * The send did not complete.
 *
 * <p>This is the one outcome where the client cannot know whether the gateway
 * took the message, which is why it is never retried automatically.
 */
public class TransportException extends PanmailException {

    private static final long serialVersionUID = 1L;

    public TransportException(String message, Throwable cause) {
        super(message, cause);
    }
}
