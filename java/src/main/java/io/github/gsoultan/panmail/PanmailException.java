package io.github.gsoultan.panmail;

/** Base class for everything this client throws. */
public class PanmailException extends RuntimeException {

    /**
     * Fixed rather than computed. javac derives one from the shape of the
     * class when it is left out, so adding a field to an exception would stop
     * a receiver on an older version of this library being able to
     * deserialise it — an InvalidClassException about a class that changed in
     * no way the receiver cares about. Every exception here declares one for
     * that reason.
     */
    private static final long serialVersionUID = 1L;

    public PanmailException(String message) {
        super(message);
    }

    public PanmailException(String message, Throwable cause) {
        super(message, cause);
    }
}
