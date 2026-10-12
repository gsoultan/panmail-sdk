package panmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Message is the mail to send.
//
// Either a body (HTML, Text, or both) or a TemplateID is required — a message
// with neither has nothing to say, and the gateway refuses it.
type Message struct {
	// ProviderID is the configured sending provider, as shown on the Email
	// Providers page. Required: panmail will not guess which of a tenant's
	// providers a message should go out through, because the wrong guess is a
	// message sent from the wrong domain.
	ProviderID string

	// From must be an address the provider is authorised to send as.
	From string

	To  []string
	Cc  []string
	Bcc []string

	Subject string

	// HTML and Text are the two bodies. Send both when you can: a text part is
	// what recipients with images off, screen readers and spam filters read.
	HTML string
	Text string

	// TemplateID renders a stored template with TemplateData instead of using
	// the bodies above. Subject comes from the template unless set here.
	TemplateID   string
	TemplateData map[string]any

	Attachments []Attachment

	// ReplyTo is where replies should go, when that is not From — a support
	// queue answering mail sent from a no-reply address, say.
	ReplyTo string

	// InReplyTo and References thread this message with an earlier one in the
	// recipient's client. InReplyTo is the Message-ID of the message being
	// answered; References is the conversation's Message-IDs so far, oldest
	// first. Each is a full Message-ID, angle brackets included:
	// <id@example.com>. At most 100 references; when they do not all fit on
	// one header line the gateway keeps the first and the most recent, as RFC
	// 5322 describes.
	InReplyTo  string
	References []string

	// ReadReceiptTo asks for a read receipt, sent to this address. It goes out
	// as the Disposition-Notification-To header, and the recipient's client
	// decides whether to honour it — most ask the reader, many never send one.
	ReadReceiptTo string

	// SaveSentCopy files a copy of the message, as sent, in the sender's Sent
	// folder, the way the tenant's Sent copies setting says to. It does
	// nothing when that setting is off.
	SaveSentCopy bool

	// IdempotencyKey makes a repeated send safe. The gateway remembers the key
	// for 24 hours alongside the message it accepted, and answers the same key
	// with the same request by returning that message rather than queueing
	// another. The same key with a different request is refused with an
	// IdempotencyKeyConflictError.
	//
	// Use something that names the thing being sent, not the attempt — an
	// order id and the kind of mail, say — so a retry from anywhere, this
	// process or the next, carries the same key. At most 255 printable ASCII
	// characters.
	//
	// Setting it is what lets WithTransportRetries repeat a send whose outcome
	// is unknown. A gateway older than the key ignores the field rather than
	// refusing it, so a key promises nothing against one; see
	// WithTransportRetries.
	IdempotencyKey string
}

// maxIdempotencyKeyLength is the gateway's bound on a key, in bytes. Keys are
// printable ASCII, so bytes and characters are the same count.
const maxIdempotencyKeyLength = 255

// maxMessageIDLength and maxReferences are the gateway's bounds on a
// Message-ID and on how many References a message may carry.
const (
	maxMessageIDLength = 255
	maxReferences      = 100
)

// ErrNoRecipients reports a message addressed to nobody.
var ErrNoRecipients = errors.New("panmail: at least one recipient is required")

// sendRequest is the wire form of a Message.
//
// The field names are the protobuf JSON names of SendEmailRequest. The gateway
// accepts the original snake_case spellings too, but camelCase is what its own
// web UI sends and so the better-travelled path.
type sendRequest struct {
	ProviderID   string           `json:"providerId"`
	From         string           `json:"from"`
	To           []string         `json:"to,omitempty"`
	Cc           []string         `json:"cc,omitempty"`
	Bcc          []string         `json:"bcc,omitempty"`
	Subject      string           `json:"subject,omitempty"`
	BodyHTML     string           `json:"bodyHtml,omitempty"`
	BodyText     string           `json:"bodyText,omitempty"`
	TemplateID   string           `json:"templateId,omitempty"`
	TemplateData map[string]any   `json:"templateData,omitempty"`
	Attachments  []attachmentJSON `json:"attachments,omitempty"`

	ReplyTo                   string   `json:"replyTo,omitempty"`
	InReplyTo                 string   `json:"inReplyTo,omitempty"`
	References                []string `json:"references,omitempty"`
	DispositionNotificationTo string   `json:"dispositionNotificationTo,omitempty"`
	SaveSentCopy              bool     `json:"saveSentCopy,omitempty"`

	// IdempotencyKey travels in the body, never as the Idempotency-Key header
	// the gateway also accepts. Go's http.Transport treats a request carrying
	// that header as safe to resend and does so by itself on a dropped
	// connection, which would be a retry this client neither counted nor
	// allowed.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// request validates the message and converts it to the wire type.
//
// Validated here as well as on the server so that the obvious mistakes cost a
// clear error rather than a round trip and a rejection phrased in the
// gateway's terms.
func (m Message) request() (*sendRequest, error) {
	if m.ProviderID == "" {
		return nil, errors.New("panmail: provider id is required")
	}
	if m.From == "" {
		return nil, errors.New("panmail: from address is required")
	}
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return nil, ErrNoRecipients
	}
	if m.HTML == "" && m.Text == "" && m.TemplateID == "" {
		return nil, errors.New("panmail: a message needs an html body, a text body or a template id")
	}

	if err := validateIdempotencyKey(m.IdempotencyKey); err != nil {
		return nil, err
	}
	if err := validateThreading(m.InReplyTo, m.References); err != nil {
		return nil, err
	}

	req := &sendRequest{
		ProviderID:     m.ProviderID,
		From:           m.From,
		To:             m.To,
		Cc:             m.Cc,
		Bcc:            m.Bcc,
		Subject:        m.Subject,
		BodyHTML:       m.HTML,
		BodyText:       m.Text,
		TemplateID:     m.TemplateID,
		TemplateData:   m.TemplateData,
		IdempotencyKey: m.IdempotencyKey,

		ReplyTo:                   m.ReplyTo,
		InReplyTo:                 m.InReplyTo,
		References:                m.References,
		DispositionNotificationTo: m.ReadReceiptTo,
		SaveSentCopy:              m.SaveSentCopy,
	}

	if len(m.TemplateData) > 0 {
		// The gateway's template_data is a google.protobuf.Struct, which is
		// JSON and nothing more. Checking it here rather than letting the
		// encoder fail mid-body means the error names the template data
		// specifically: "unsupported type" from the middle of a marshal sends
		// people looking in the wrong place.
		if _, err := json.Marshal(m.TemplateData); err != nil {
			return nil, fmt.Errorf("panmail: template data is not representable as JSON: %w", err)
		}
	}

	for _, attachment := range m.Attachments {
		if attachment.Filename == "" {
			return nil, errors.New("panmail: every attachment needs a filename")
		}
		if err := validateContentID(attachment.ContentID); err != nil {
			return nil, fmt.Errorf("panmail: attachment %q: %w", attachment.Filename, err)
		}
		req.Attachments = append(req.Attachments, attachment.wire())
	}

	return req, nil
}

// validateIdempotencyKey applies the gateway's own rule, so a key it would
// refuse costs a clear error here rather than a round trip.
func validateIdempotencyKey(key string) error {
	if len(key) > maxIdempotencyKeyLength {
		return fmt.Errorf("panmail: the idempotency key is %d characters long; the limit is %d",
			len(key), maxIdempotencyKeyLength)
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c < 0x20 || c > 0x7e {
			return fmt.Errorf("panmail: the idempotency key may contain only printable ASCII; byte %d is not", i)
		}
	}
	return nil
}

// validateThreading applies the gateway's rules for In-Reply-To and
// References. The addresses — ReplyTo, ReadReceiptTo — are left to the
// gateway, as From and the recipients are: its parser is the one that counts.
func validateThreading(inReplyTo string, references []string) error {
	if inReplyTo != "" {
		if err := validateMessageID(inReplyTo); err != nil {
			return fmt.Errorf("panmail: in-reply-to: %w", err)
		}
	}
	if len(references) > maxReferences {
		return fmt.Errorf("panmail: %d references; the limit is %d", len(references), maxReferences)
	}
	for _, id := range references {
		if err := validateMessageID(id); err != nil {
			return fmt.Errorf("panmail: references: %w", err)
		}
	}
	return nil
}

// validateMessageID checks the shape RFC 5322 gives a msg-id, <left@right> in
// printable ASCII without spaces — the same check the gateway makes.
func validateMessageID(id string) error {
	if len(id) > maxMessageIDLength {
		return fmt.Errorf("the Message-ID is %d characters long; the limit is %d", len(id), maxMessageIDLength)
	}
	if len(id) < 5 || id[0] != '<' || id[len(id)-1] != '>' {
		return fmt.Errorf("the Message-ID %q must be in angle brackets, like <id@example.com>", id)
	}
	inner := id[1 : len(id)-1]
	for i := 0; i < len(inner); i++ {
		if c := inner[i]; c <= ' ' || c > '~' || c == '<' || c == '>' {
			return fmt.Errorf("the Message-ID %q may contain only printable ASCII without spaces", id)
		}
	}
	if at := strings.IndexByte(inner, '@'); at <= 0 || at == len(inner)-1 {
		return fmt.Errorf("the Message-ID %q must have the form <left@right>", id)
	}
	return nil
}

// validateContentID applies the gateway's rule for an inline attachment's
// Content-ID: one token of printable ASCII, without the angle brackets the
// header adds.
func validateContentID(id string) error {
	if len(id) > maxMessageIDLength {
		return fmt.Errorf("content id is %d characters long; the limit is %d", len(id), maxMessageIDLength)
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; c <= ' ' || c > '~' || c == '<' || c == '>' {
			return errors.New("content id may contain only printable ASCII without spaces or angle brackets")
		}
	}
	return nil
}
