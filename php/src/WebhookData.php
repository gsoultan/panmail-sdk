<?php

declare(strict_types=1);

namespace Panmail;

/**
 * Decodes a webhook's data into its typed payload, as strictly as Go's
 * encoding/json does in the Go client: a field may be absent or null, but one
 * that is present must be the type the gateway writes. A payload half decoded
 * would hand back zero values nobody can tell from real ones.
 *
 * The field names are the gateway's Go struct tags, not its protos: it
 * marshals webhook payloads with encoding/json. Hence snake_case, and a
 * delivery event's type as a number.
 *
 * @internal
 */
final class WebhookData
{
    /**
     * EmailEventType by number, checked against the "numbers" of
     * testdata/event-types.json.
     */
    private const STATUS_NUMBERS = [
        0 => Status::UNSPECIFIED,
        1 => Status::SENT,
        2 => Status::DELIVERED,
        3 => Status::OPENED,
        4 => Status::CLICKED,
        5 => Status::BOUNCED,
        6 => Status::HARD_BOUNCE,
        7 => Status::SOFT_BOUNCE,
        8 => Status::SPAM_REPORT,
        9 => Status::UNSUBSCRIBED,
        10 => Status::DROPPED,
        11 => Status::REJECTED,
        12 => Status::PENDING,
        13 => Status::DEFERRED,
        14 => Status::COMPLAINED,
    ];

    /** @throws \UnexpectedValueException */
    public static function mailEvent(mixed $data): MailEvent
    {
        $d = self::object($data, 'the data');

        return new MailEvent(
            id: self::string($d, 'id'),
            messageId: self::string($d, 'message_id'),
            providerId: self::string($d, 'provider_id'),
            providerName: self::string($d, 'provider_name'),
            type: self::status($d['type'] ?? null),
            recipient: self::string($d, 'recipient'),
            subject: self::string($d, 'subject'),
            timestamp: self::time($d, 'timestamp'),
            reason: self::string($d, 'error_message'),
            metadata: ($d['metadata'] ?? null) === null ? [] : self::object($d['metadata'], 'metadata'),
        );
    }

    /** @throws \UnexpectedValueException */
    public static function mailHeld(mixed $data): MailHeld
    {
        $d = self::object($data, 'the data');

        $matched = [];
        foreach (self::list($d, 'matched') as $i => $item) {
            if ($item === null) {
                // Go decodes a null element to a zero value, not an error.
                $item = [];
            }
            $m = self::object($item, "matched[$i]");
            $matched[] = new MatchedCondition(
                field: self::string($m, 'field'),
                operator: self::string($m, 'operator'),
                values: self::strings($m, 'values'),
                header: self::string($m, 'header'),
                number: self::int($m, 'number'),
            );
        }

        return new MailHeld(
            reviewId: self::string($d, 'id'),
            direction: self::string($d, 'direction'),
            messageId: self::string($d, 'message_id'),
            ruleId: self::string($d, 'rule_id'),
            ruleName: self::string($d, 'rule_name'),
            from: self::string($d, 'from'),
            recipients: self::strings($d, 'recipients'),
            subject: self::string($d, 'subject'),
            sizeBytes: self::int($d, 'size_bytes'),
            attachmentCount: self::int($d, 'attachment_count'),
            attachmentNames: self::strings($d, 'attachment_names'),
            matched: $matched,
            heldAt: self::time($d, 'held_at'),
            expiresAt: self::time($d, 'expires_at'),
        );
    }

    /** @throws \UnexpectedValueException */
    public static function mailHoldOutcome(mixed $data): MailHoldOutcome
    {
        $d = self::object($data, 'the data');

        return new MailHoldOutcome(
            reviewId: self::string($d, 'id'),
            direction: self::string($d, 'direction'),
            messageId: self::string($d, 'message_id'),
            ruleId: self::string($d, 'rule_id'),
            ruleName: self::string($d, 'rule_name'),
            from: self::string($d, 'from'),
            recipients: self::strings($d, 'recipients'),
            subject: self::string($d, 'subject'),
            heldAt: self::time($d, 'held_at'),
            decidedAt: self::time($d, 'decided_at'),
            reviewedBy: self::string($d, 'reviewed_by'),
            note: self::string($d, 'note'),
        );
    }

    /** @throws \UnexpectedValueException */
    public static function inboundMail(mixed $data): InboundMail
    {
        $d = self::object($data, 'the data');

        $headers = [];
        $raw = $d['headers'] ?? null;
        if ($raw !== null) {
            foreach (self::object($raw, 'headers') as $name => $value) {
                if ($value !== null && !is_string($value)) {
                    throw new \UnexpectedValueException("header $name is not a string");
                }
                $headers[(string) $name] = (string) $value;
            }
        }

        return new InboundMail(
            id: self::string($d, 'id'),
            from: self::string($d, 'from'),
            to: self::strings($d, 'to'),
            subject: self::string($d, 'subject'),
            html: self::string($d, 'body_html'),
            text: self::string($d, 'body_text'),
            timestamp: self::protoTimestamp($d['timestamp'] ?? null),
            headers: $headers,
        );
    }

    /**
     * A JSON object. An empty one decodes to an empty PHP array, which is
     * indistinguishable from [] and so accepted as one.
     *
     * @return array<mixed>
     */
    private static function object(mixed $value, string $what): array
    {
        if ($value === null) {
            throw new \UnexpectedValueException("there is no $what");
        }
        if (!is_array($value) || ($value !== [] && array_is_list($value))) {
            throw new \UnexpectedValueException("$what is not an object");
        }

        return $value;
    }

    /** @param array<mixed> $d */
    private static function string(array $d, string $key): string
    {
        $value = $d[$key] ?? null;
        if ($value !== null && !is_string($value)) {
            throw new \UnexpectedValueException("$key is not a string");
        }

        return (string) $value;
    }

    /** @param array<mixed> $d */
    private static function int(array $d, string $key): int
    {
        $value = $d[$key] ?? null;
        if ($value !== null && !is_int($value)) {
            throw new \UnexpectedValueException("$key is not a whole number");
        }

        return (int) $value;
    }

    /**
     * @param array<mixed> $d
     *
     * @return array<mixed>
     */
    private static function list(array $d, string $key): array
    {
        $value = $d[$key] ?? null;
        if ($value === null) {
            return [];
        }
        if (!is_array($value) || !array_is_list($value)) {
            throw new \UnexpectedValueException("$key is not a list");
        }

        return $value;
    }

    /**
     * @param array<mixed> $d
     *
     * @return list<string>
     */
    private static function strings(array $d, string $key): array
    {
        $out = [];
        foreach (self::list($d, $key) as $i => $value) {
            if ($value !== null && !is_string($value)) {
                throw new \UnexpectedValueException("{$key}[$i] is not a string");
            }
            $out[] = (string) $value;
        }

        return $out;
    }

    /** @param array<mixed> $d */
    private static function time(array $d, string $key): ?\DateTimeImmutable
    {
        $value = $d[$key] ?? null;
        if ($value === null) {
            return null;
        }
        $parsed = is_string($value) ? Rfc3339::parse($value) : null;
        if ($parsed === null) {
            throw new \UnexpectedValueException("$key is not an RFC 3339 time");
        }

        return $parsed;
    }

    /**
     * A google.protobuf.Timestamp as encoding/json writes the generated
     * struct — {"seconds": n, "nanos": n}, either left out when zero — or as
     * protojson writes it, an RFC 3339 string.
     */
    private static function protoTimestamp(mixed $value): ?\DateTimeImmutable
    {
        if ($value === null) {
            return null;
        }
        if (is_string($value)) {
            return Rfc3339::parse($value)
                ?? throw new \UnexpectedValueException('timestamp is not an RFC 3339 time');
        }
        $parts = self::object($value, 'timestamp');
        $seconds = self::int($parts, 'seconds');
        $nanos = self::int($parts, 'nanos');
        if ($nanos < 0 || $nanos > 999_999_999) {
            throw new \UnexpectedValueException("timestamp's nanos must be 0 to 999999999, not $nanos");
        }
        $micros = intdiv($nanos, 1000);
        $time = \DateTimeImmutable::createFromFormat('U.u', sprintf('%d.%06d', $seconds, $micros));
        if ($time === false) {
            throw new \UnexpectedValueException('timestamp is out of range');
        }

        return $time;
    }

    /**
     * A delivery event's type: the gateway sends the EmailEventType number,
     * because encoding/json knows nothing of enum names. A name is taken as it
     * is, in case that ever changes.
     */
    private static function status(mixed $value): string
    {
        if ($value === null || $value === '') {
            return Status::UNSPECIFIED;
        }
        if (is_int($value)) {
            return self::STATUS_NUMBERS[$value] ?? (string) $value;
        }
        if (is_string($value) || is_float($value)) {
            return (string) $value;
        }
        throw new \UnexpectedValueException('type is not a name or a number');
    }
}
