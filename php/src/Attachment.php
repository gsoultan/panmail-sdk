<?php

declare(strict_types=1);

namespace Panmail;

/** A file to send with a message. */
final class Attachment
{
    public function __construct(
        /** Filename as the recipient sees it. Required. */
        public readonly string $filename,

        /** The raw bytes, not base64: the transport encodes them. */
        public readonly string $content,

        /**
         * Content type of the bytes. Left null it is guessed from the
         * filename's extension, and falls back to application/octet-stream —
         * which every mail client will offer as a download rather than display
         * inline, so set it when you know it.
         */
        public readonly ?string $contentType = null,
    ) {
    }

    /** @return array<string, string> */
    public function toArray(): array
    {
        return [
            'filename' => $this->filename,
            'contentType' => $this->contentType ?? $this->guessContentType(),
            // A bytes field is base64 in protobuf JSON, which is what the
            // gateway decodes. Raw bytes would arrive as mojibake.
            'content' => base64_encode($this->content),
        ];
    }

    private function guessContentType(): string
    {
        $extension = strtolower(pathinfo($this->filename, PATHINFO_EXTENSION));

        return match ($extension) {
            'pdf' => 'application/pdf',
            // The text types carry a charset, because Go's mime package appends
            // one to every text/* it returns and three clients of one gateway
            // should not disagree about a header. It is also the better answer:
            // a text attachment with no declared encoding is one the mail
            // client has to guess at, and a UTF-8 CSV guessed as latin-1 is
            // mojibake in a spreadsheet.
            'csv' => 'text/csv; charset=utf-8',
            'txt' => 'text/plain; charset=utf-8',
            'html', 'htm' => 'text/html; charset=utf-8',
            'json' => 'application/json',
            'png' => 'image/png',
            'jpg', 'jpeg' => 'image/jpeg',
            'gif' => 'image/gif',
            'svg' => 'image/svg+xml',
            'zip' => 'application/zip',
            'ics' => 'text/calendar; charset=utf-8',
            default => 'application/octet-stream',
        };
    }
}
