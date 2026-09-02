import { InvalidMessageError } from './errors.js';

/**
 * The state a message is in.
 *
 * The values are the gateway's own enum names, carried verbatim: the wire
 * format is protobuf JSON, which encodes an enum as its name rather than its
 * number. Comparing against these is comparing against exactly what the
 * gateway sent.
 */
export const Status = {
  /**
   * What a successful send reports: the gateway has the message on disk and
   * will deliver it. Delivery itself is reported later, through events and
   * webhooks.
   */
  Pending: 'EMAIL_EVENT_TYPE_PENDING',

  /**
   * The rest are what the gateway reports afterwards, on the event stream and
   * through webhooks, keyed by result.messageId. A send never returns them.
   *
   * Bounced is the general refusal; HardBounce and SoftBounce are the gateway
   * saying which kind, when the provider told it. A hard bounce is an address
   * that will never accept mail and should be suppressed; a soft one is a
   * mailbox that was full or a server that was down.
   */
  Sent: 'EMAIL_EVENT_TYPE_SENT',
  Delivered: 'EMAIL_EVENT_TYPE_DELIVERED',
  Opened: 'EMAIL_EVENT_TYPE_OPENED',
  Clicked: 'EMAIL_EVENT_TYPE_CLICKED',
  Bounced: 'EMAIL_EVENT_TYPE_BOUNCED',
  HardBounce: 'EMAIL_EVENT_TYPE_HARD_BOUNCE',
  SoftBounce: 'EMAIL_EVENT_TYPE_SOFT_BOUNCE',
  SpamReport: 'EMAIL_EVENT_TYPE_SPAM_REPORT',
  Unsubscribed: 'EMAIL_EVENT_TYPE_UNSUBSCRIBED',
  Dropped: 'EMAIL_EVENT_TYPE_DROPPED',
  Rejected: 'EMAIL_EVENT_TYPE_REJECTED',
  Deferred: 'EMAIL_EVENT_TYPE_DEFERRED',
  Complained: 'EMAIL_EVENT_TYPE_COMPLAINED',

  /**
   * protobuf's zero value, not a state a message is ever in. Here because the
   * gateway's enum has it, and a client that quietly omitted a value would be
   * the start of the two drifting.
   */
  Unspecified: 'EMAIL_EVENT_TYPE_UNSPECIFIED',
} as const;

export type Status = (typeof Status)[keyof typeof Status] | (string & {});

/** What the gateway accepted. */
export interface Result {
  /**
   * Identifies the message for the rest of its life: delivery events, webhook
   * notifications and the analytics pages are all keyed by it. Worth storing
   * next to whatever prompted the send.
   */
  messageId: string;

  /**
   * The status at the moment of acceptance, which for a queued message is
   * Status.Pending.
   */
  status: Status;
}

/** A file to send with a message. */
export interface Attachment {
  /** Filename as the recipient sees it. Required. */
  filename: string;

  /** The raw bytes, not base64: the transport encodes them. */
  content: Uint8Array | string;

  /**
   * Content type of the bytes. Left unset it is guessed from the filename's
   * extension, and falls back to application/octet-stream — which every mail
   * client will offer as a download rather than display inline, so set it when
   * you know it.
   */
  contentType?: string;
}

/**
 * The mail to send.
 *
 * Either a body (html, text, or both) or a templateId is required — a message
 * with neither has nothing to say, and the gateway refuses it.
 */
export interface Message {
  /**
   * The configured sending provider, as shown on the Email Providers page.
   * Required: panmail will not guess which of a tenant's providers a message
   * goes out through, because the wrong guess is a message sent from the wrong
   * domain.
   */
  providerId: string;

  /** Must be an address the provider is authorised to send as. */
  from: string;

  to?: string[];
  cc?: string[];
  bcc?: string[];

  subject?: string;

  /**
   * Send both bodies when you can: a text part is what recipients with images
   * off, screen readers and spam filters read.
   */
  html?: string;
  text?: string;

  /**
   * Renders a stored template with templateData instead of using the bodies
   * above. Subject comes from the template unless set here.
   */
  templateId?: string;
  templateData?: Record<string, unknown>;

  attachments?: Attachment[];
}

const CONTENT_TYPES: Record<string, string> = {
  pdf: 'application/pdf',
  // The text types carry a charset, because Go's mime package appends one to
  // every text/* it returns and three clients of one gateway should not disagree
  // about a header. It is also the better answer: a text attachment with no
  // declared encoding is one the mail client has to guess at, and a UTF-8 CSV
  // guessed as latin-1 is mojibake in a spreadsheet.
  csv: 'text/csv; charset=utf-8',
  txt: 'text/plain; charset=utf-8',
  html: 'text/html; charset=utf-8',
  htm: 'text/html; charset=utf-8',
  json: 'application/json',
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  svg: 'image/svg+xml',
  zip: 'application/zip',
  ics: 'text/calendar; charset=utf-8',
};

function guessContentType(filename: string): string {
  const extension = filename.split('.').pop()?.toLowerCase() ?? '';
  return CONTENT_TYPES[extension] ?? 'application/octet-stream';
}

function toBase64(content: Uint8Array | string): string {
  const bytes = typeof content === 'string' ? new TextEncoder().encode(content) : content;
  return Buffer.from(bytes).toString('base64');
}

/**
 * Validates the message and converts it to the wire form.
 *
 * The field names are the protobuf JSON names of SendEmailRequest. The gateway
 * accepts the original snake_case spellings too, but camelCase is what its own
 * web UI sends and so the better-travelled path.
 */
export function toWire(message: Message): Record<string, unknown> {
  const to = message.to ?? [];
  const cc = message.cc ?? [];
  const bcc = message.bcc ?? [];

  if (!message.providerId) {
    throw new InvalidMessageError('panmail: provider id is required');
  }
  if (!message.from) {
    throw new InvalidMessageError('panmail: from address is required');
  }
  if (to.length + cc.length + bcc.length === 0) {
    throw new InvalidMessageError('panmail: at least one recipient is required');
  }
  if (!message.html && !message.text && !message.templateId) {
    throw new InvalidMessageError(
      'panmail: a message needs an html body, a text body or a template id',
    );
  }

  const wire: Record<string, unknown> = {
    providerId: message.providerId,
    from: message.from,
  };

  if (to.length) wire.to = to;
  if (cc.length) wire.cc = cc;
  if (bcc.length) wire.bcc = bcc;
  if (message.subject) wire.subject = message.subject;
  if (message.html) wire.bodyHtml = message.html;
  if (message.text) wire.bodyText = message.text;
  if (message.templateId) wire.templateId = message.templateId;

  // The gateway's template_data is a google.protobuf.Struct, which is JSON and
  // nothing more.
  if (message.templateData && Object.keys(message.templateData).length > 0) {
    wire.templateData = message.templateData;
  }

  const attachments = message.attachments ?? [];
  if (attachments.length > 0) {
    wire.attachments = attachments.map((attachment) => {
      if (!attachment.filename) {
        throw new InvalidMessageError('panmail: every attachment needs a filename');
      }
      return {
        filename: attachment.filename,
        contentType: attachment.contentType || guessContentType(attachment.filename),
        // A bytes field is base64 in protobuf JSON, which is what the gateway
        // decodes. Raw bytes would arrive as mojibake.
        content: toBase64(attachment.content),
      };
    });
  }

  return wire;
}
