package io.github.gsoultan.panmail;

import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;

/** A file to send with a message. */
public final class Attachment {

    private static final Map<String, String> CONTENT_TYPES = Map.ofEntries(
            Map.entry("pdf", "application/pdf"),
            // The text types carry a charset, because Go's mime package appends
            // one to every text/* it returns and four clients of one gateway
            // should not disagree about a header. It is also the better answer:
            // a text attachment with no declared encoding is one the mail client
            // has to guess at, and a UTF-8 CSV guessed as latin-1 is mojibake in
            // a spreadsheet.
            Map.entry("csv", "text/csv; charset=utf-8"),
            Map.entry("txt", "text/plain; charset=utf-8"),
            Map.entry("html", "text/html; charset=utf-8"),
            Map.entry("htm", "text/html; charset=utf-8"),
            Map.entry("json", "application/json"),
            Map.entry("png", "image/png"),
            Map.entry("jpg", "image/jpeg"),
            Map.entry("jpeg", "image/jpeg"),
            Map.entry("gif", "image/gif"),
            Map.entry("svg", "image/svg+xml"),
            Map.entry("zip", "application/zip"),
            Map.entry("ics", "text/calendar; charset=utf-8"));

    private final String filename;
    private final byte[] content;
    private final String contentType;

    /**
     * @param filename    as the recipient sees it. Required.
     * @param content     the raw bytes, not base64: the transport encodes them.
     * @param contentType left null it is guessed from the filename's extension,
     *                    and falls back to application/octet-stream — which
     *                    every mail client will offer as a download rather than
     *                    display inline, so set it when you know it.
     */
    public Attachment(String filename, byte[] content, String contentType) {
        this.filename = Objects.requireNonNullElse(filename, "");
        this.content = content == null ? new byte[0] : content.clone();
        this.contentType = contentType;
    }

    public Attachment(String filename, byte[] content) {
        this(filename, content, null);
    }

    public Attachment(String filename, String content) {
        this(filename, content == null ? null : content.getBytes(StandardCharsets.UTF_8), null);
    }

    public String filename() {
        return filename;
    }

    public byte[] content() {
        return content.clone();
    }

    String resolvedContentType() {
        if (contentType != null && !contentType.isEmpty()) {
            return contentType;
        }
        int dot = filename.lastIndexOf('.');
        String extension = dot < 0 ? "" : filename.substring(dot + 1).toLowerCase(Locale.ROOT);
        return CONTENT_TYPES.getOrDefault(extension, "application/octet-stream");
    }
}
