package io.github.gsoultan.panmail;

/** The API key was missing, rejected, or lacks the email:send scope. */
public class AuthException extends ApiException {

    private static final long serialVersionUID = 1L;

    public AuthException(String message, String code, int status) {
        super(message, code, status);
    }
}
