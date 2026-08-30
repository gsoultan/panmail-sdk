package io.github.gsoultan.panmail;

/**
 * A message this client refused before sending it.
 *
 * <p>Validated locally as well as on the server so that the obvious mistakes
 * cost a clear error rather than a round trip and a rejection phrased in the
 * gateway's terms.
 */
public class InvalidMessageException extends PanmailException {

    private static final long serialVersionUID = 1L;

    public InvalidMessageException(String message) {
        super(message);
    }
}
