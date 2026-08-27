package panmail

import (
	"mime"
	"path/filepath"
)

// Attachment is a file to send with a message.
type Attachment struct {
	// Filename as the recipient sees it. Required.
	Filename string

	// ContentType of the bytes. Left empty it is guessed from the filename's
	// extension, and falls back to application/octet-stream — which every mail
	// client will offer to display as a download rather than inline, so set it
	// when you know it.
	ContentType string

	// Content is the raw bytes, not base64: the transport encodes them.
	Content []byte
}

// attachmentJSON is the wire form. Content is []byte, which encoding/json
// base64-encodes — the same representation protobuf JSON gives a bytes field,
// which is what the gateway decodes.
type attachmentJSON struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType,omitempty"`
	Content     []byte `json:"content,omitempty"`
}

func (a Attachment) wire() attachmentJSON {
	contentType := a.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(a.Filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return attachmentJSON{
		Filename:    a.Filename,
		ContentType: contentType,
		Content:     a.Content,
	}
}
