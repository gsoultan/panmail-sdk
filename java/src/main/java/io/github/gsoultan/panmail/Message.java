package io.github.gsoultan.panmail;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The mail to send.
 *
 * <p>Either a body (html, text, or both) or a template id is required — a
 * message with neither has nothing to say, and the gateway refuses it.
 *
 * <pre>{@code
 * Message message = Message.builder()
 *         .providerId("0f8b...")
 *         .from("noreply@example.com")
 *         .to("someone@example.org")
 *         .subject("Your receipt")
 *         .html("<p>Thanks for your order.</p>")
 *         .build();
 * }</pre>
 */
public final class Message {

    private final String providerId;
    private final String from;
    private final List<String> to;
    private final List<String> cc;
    private final List<String> bcc;
    private final String subject;
    private final String html;
    private final String text;
    private final String templateId;
    private final Map<String, Object> templateData;
    private final List<Attachment> attachments;

    private Message(Builder builder) {
        this.providerId = builder.providerId;
        this.from = builder.from;
        this.to = List.copyOf(builder.to);
        this.cc = List.copyOf(builder.cc);
        this.bcc = List.copyOf(builder.bcc);
        this.subject = builder.subject;
        this.html = builder.html;
        this.text = builder.text;
        this.templateId = builder.templateId;
        this.templateData = Map.copyOf(builder.templateData);
        this.attachments = List.copyOf(builder.attachments);
    }

    public static Builder builder() {
        return new Builder();
    }

    /**
     * Validates the message and converts it to the wire form.
     *
     * <p>The field names are the protobuf JSON names of SendEmailRequest. The
     * gateway accepts the original snake_case spellings too, but camelCase is
     * what its own web UI sends and so the better-travelled path.
     *
     * @throws InvalidMessageException when the message could not be sent as it stands
     */
    Map<String, Object> toWire() {
        if (isBlank(providerId)) {
            throw new InvalidMessageException("panmail: provider id is required");
        }
        if (isBlank(from)) {
            throw new InvalidMessageException("panmail: from address is required");
        }
        if (to.size() + cc.size() + bcc.size() == 0) {
            throw new InvalidMessageException("panmail: at least one recipient is required");
        }
        if (isBlank(html) && isBlank(text) && isBlank(templateId)) {
            throw new InvalidMessageException(
                    "panmail: a message needs an html body, a text body or a template id");
        }

        Map<String, Object> wire = new LinkedHashMap<>();
        wire.put("providerId", providerId);
        wire.put("from", from);
        if (!to.isEmpty()) {
            wire.put("to", to);
        }
        if (!cc.isEmpty()) {
            wire.put("cc", cc);
        }
        if (!bcc.isEmpty()) {
            wire.put("bcc", bcc);
        }
        if (!isBlank(subject)) {
            wire.put("subject", subject);
        }
        if (!isBlank(html)) {
            wire.put("bodyHtml", html);
        }
        if (!isBlank(text)) {
            wire.put("bodyText", text);
        }
        if (!isBlank(templateId)) {
            wire.put("templateId", templateId);
        }
        if (!templateData.isEmpty()) {
            // The gateway's template_data is a google.protobuf.Struct, which is
            // JSON and nothing more.
            wire.put("templateData", templateData);
        }

        if (!attachments.isEmpty()) {
            List<Map<String, Object>> encoded = new ArrayList<>(attachments.size());
            for (Attachment attachment : attachments) {
                if (isBlank(attachment.filename())) {
                    throw new InvalidMessageException("panmail: every attachment needs a filename");
                }
                Map<String, Object> entry = new LinkedHashMap<>();
                entry.put("filename", attachment.filename());
                entry.put("contentType", attachment.resolvedContentType());
                // Jackson serialises a byte[] as base64, which is what a bytes
                // field is in protobuf JSON and what the gateway decodes.
                entry.put("content", attachment.content());
                encoded.add(entry);
            }
            wire.put("attachments", encoded);
        }

        return wire;
    }

    private static boolean isBlank(String value) {
        return value == null || value.isEmpty();
    }

    /** Builds a {@link Message}. */
    public static final class Builder {

        private String providerId = "";
        private String from = "";
        private final List<String> to = new ArrayList<>();
        private final List<String> cc = new ArrayList<>();
        private final List<String> bcc = new ArrayList<>();
        private String subject = "";
        private String html = "";
        private String text = "";
        private String templateId = "";
        private final Map<String, Object> templateData = new LinkedHashMap<>();
        private final List<Attachment> attachments = new ArrayList<>();

        /**
         * The configured sending provider, as shown on the Email Providers
         * page. Required: panmail will not guess which of a tenant's providers
         * a message goes out through, because the wrong guess is a message sent
         * from the wrong domain.
         */
        public Builder providerId(String providerId) {
            this.providerId = providerId;
            return this;
        }

        /** Must be an address the provider is authorised to send as. */
        public Builder from(String from) {
            this.from = from;
            return this;
        }

        public Builder to(String... addresses) {
            this.to.addAll(List.of(addresses));
            return this;
        }

        public Builder to(Collection<String> addresses) {
            this.to.addAll(addresses);
            return this;
        }

        public Builder cc(String... addresses) {
            this.cc.addAll(List.of(addresses));
            return this;
        }

        public Builder cc(Collection<String> addresses) {
            this.cc.addAll(addresses);
            return this;
        }

        public Builder bcc(String... addresses) {
            this.bcc.addAll(List.of(addresses));
            return this;
        }

        public Builder bcc(Collection<String> addresses) {
            this.bcc.addAll(addresses);
            return this;
        }

        public Builder subject(String subject) {
            this.subject = subject;
            return this;
        }

        /**
         * Send both bodies when you can: a text part is what recipients with
         * images off, screen readers and spam filters read.
         */
        public Builder html(String html) {
            this.html = html;
            return this;
        }

        public Builder text(String text) {
            this.text = text;
            return this;
        }

        /**
         * Renders a stored template with the template data instead of using the
         * bodies above. Subject comes from the template unless set here.
         */
        public Builder templateId(String templateId) {
            this.templateId = templateId;
            return this;
        }

        public Builder templateData(Map<String, Object> templateData) {
            this.templateData.putAll(templateData);
            return this;
        }

        public Builder templateData(String key, Object value) {
            this.templateData.put(key, value);
            return this;
        }

        public Builder attach(Attachment... attachments) {
            this.attachments.addAll(List.of(attachments));
            return this;
        }

        public Message build() {
            return new Message(this);
        }
    }
}
