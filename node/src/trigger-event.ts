/**
 * Why a webhook fired.
 *
 * The values are the gateway's own `WebhookTriggerEvent` enum names, carried
 * verbatim: it dispatches with `event.String()`, so what arrives in the
 * `X-Panmail-Event` header and the envelope's `event` field is the enum name and
 * nothing else.
 *
 * Not to be confused with `Status`, which names the state a message is in and is
 * what a send returns. They are different enums in the gateway and neither is a
 * superset of the other.
 */
export const TriggerEvent = {
  /** MailSent through MailBounced are the delivery pipeline reporting what a provider did. */
  MailSent: 'WEBHOOK_TRIGGER_EVENT_MAIL_SENT',
  MailDelivered: 'WEBHOOK_TRIGGER_EVENT_MAIL_DELIVERED',
  MailOpened: 'WEBHOOK_TRIGGER_EVENT_MAIL_OPENED',
  MailClicked: 'WEBHOOK_TRIGGER_EVENT_MAIL_CLICKED',
  MailBounced: 'WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED',

  /**
   * A provider refusing a send. Not `MailQuarantineRejected`, which is a person
   * refusing a held message — a subscriber acting on "rejected" needs to know
   * which one it received.
   */
  MailRejected: 'WEBHOOK_TRIGGER_EVENT_MAIL_REJECTED',

  MailInbound: 'WEBHOOK_TRIGGER_EVENT_MAIL_INBOUND',

  /**
   * A filter rule quarantined a message for review. Worth subscribing to:
   * without it a hold is silent, and a message nobody reviewed expires unseen —
   * worse than one that was refused, because nobody ever decided it.
   */
  MailHeld: 'WEBHOOK_TRIGGER_EVENT_MAIL_HELD',

  /** The three ways a hold ends. */
  MailReleased: 'WEBHOOK_TRIGGER_EVENT_MAIL_RELEASED',
  MailQuarantineRejected: 'WEBHOOK_TRIGGER_EVENT_MAIL_QUARANTINE_REJECTED',

  /**
   * A held message reached its retention deadline with nobody having decided.
   * This is the one to alert on: it does not say a message was refused, it says
   * a queue went unwatched.
   */
  MailExpired: 'WEBHOOK_TRIGGER_EVENT_MAIL_EXPIRED',

  /**
   * protobuf's zero value, not a reason a webhook ever fires. Here because the
   * gateway's enum has it, and a client that quietly omitted a value would be
   * the start of the two drifting.
   */
  Unspecified: 'WEBHOOK_TRIGGER_EVENT_UNSPECIFIED',
} as const;

export type TriggerEvent = (typeof TriggerEvent)[keyof typeof TriggerEvent] | (string & {});
