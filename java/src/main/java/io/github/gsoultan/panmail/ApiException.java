package io.github.gsoultan.panmail;

/**
 * A refusal this client has no more specific type for.
 *
 * <p>{@link #code()} is the Connect error code the gateway sent —
 * "invalid_argument", "internal" and so on — and {@link #status()} is the HTTP
 * status it arrived with.
 */
public class ApiException extends PanmailException {

    private static final long serialVersionUID = 1L;

    private final String code;
    private final int status;

    public ApiException(String message, String code, int status) {
        super(message);
        this.code = code;
        this.status = status;
    }

    public String code() {
        return code;
    }

    public int status() {
        return status;
    }
}
