package io.github.gsoultan.panmail;

/** Base class for everything this client throws. */
public class PanmailException extends RuntimeException {

    public PanmailException(String message) {
        super(message);
    }

    public PanmailException(String message, Throwable cause) {
        super(message, cause);
    }
}
