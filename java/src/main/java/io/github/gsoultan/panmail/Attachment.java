package io.github.gsoultan.panmail;

import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;

/** A file to send with a message. */
public final class Attachment {

    private static final Map<String, String> CONTENT_TYPES = Map.ofEntries(
            Map.entry("pdf", "application/pdf"),
            Map.entry("csv", "text/csv"),
            Map.entry("txt", "text/plain"),
            Map.entry("html", "text/html"),
            Map.entry("htm", "text/html"),
            Map.entry("json", "application/json"),
            Map.entry("png", "image/png"),
            Map.entry("jpg", "image/jpeg"),
            Map.entry("jpeg", "image/jpeg"),
            Map.entry("gif", "image/gif"),
            Map.entry("svg", "image/svg+xml"),
            Map.entry("zip", "application/zip"),
            Map.entry("ics", "text/calendar"));

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
