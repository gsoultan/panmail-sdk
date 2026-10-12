package panmail_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	panmail "github.com/gsoultan/panmail-sdk"
)

// reply is a message answering an earlier one, with every field a mail client
// sets on a reply.
func reply() panmail.Message {
	msg := hello()
	msg.ReplyTo = "support@example.com"
	msg.InReplyTo = "<b@example.org>"
	msg.References = []string{"<a@example.org>", "<b@example.org>"}
	msg.ReadReceiptTo = "app@example.com"
	msg.SaveSentCopy = true
	return msg
}

func TestSendCarriesTheReplyFields(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), reply()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := g.bodies[0]
	for field, want := range map[string]string{
		"replyTo":                   "support@example.com",
		"inReplyTo":                 "<b@example.org>",
		"dispositionNotificationTo": "app@example.com",
	} {
		if got, _ := body[field].(string); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if got := fmt.Sprint(body["references"]); got != "[<a@example.org> <b@example.org>]" {
		t.Errorf("references = %s, want both ids, oldest first", got)
	}
	if body["saveSentCopy"] != true {
		t.Errorf("saveSentCopy = %v, want true", body["saveSentCopy"])
	}
}

func TestSendLeavesOutAbsentReplyFields(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, field := range []string{"replyTo", "inReplyTo", "references", "dispositionNotificationTo", "saveSentCopy"} {
		if _, present := g.bodies[0][field]; present {
			t.Errorf("%s was sent for a message that does not set it", field)
		}
	}
}

func TestSendRejectsAnUnusableMessageID(t *testing.T) {
	cases := map[string]string{
		"no brackets":   "b@example.org",
		"no at":         "<b.example.org>",
		"nothing left":  "<@example.org>",
		"nothing right": "<b@>",
		"a space":       "<b @example.org>",
		"a newline":     "<b@example.org>\r\nBcc: x@example.net",
		"not ascii":     "<é@example.org>",
		"too long":      "<" + strings.Repeat("b", 250) + "@example.org>",
	}

	for name, id := range cases {
		for _, field := range []string{"in-reply-to", "references"} {
			t.Run(name+"/"+field, func(t *testing.T) {
				g := accepts(t)
				msg := hello()
				if field == "in-reply-to" {
					msg.InReplyTo = id
				} else {
					msg.References = []string{"<a@example.org>", id}
				}

				if _, err := client(t, g).Send(context.Background(), msg); err == nil {
					t.Fatal("Send accepted a Message-ID the gateway refuses")
				}
				if g.calls != 0 {
					t.Errorf("the gateway was called %d times, want 0", g.calls)
				}
			})
		}
	}
}

func TestSendRejectsTooManyReferences(t *testing.T) {
	g := accepts(t)
	msg := hello()
	for i := range 101 {
		msg.References = append(msg.References, fmt.Sprintf("<%d@example.org>", i))
	}

	if _, err := client(t, g).Send(context.Background(), msg); err == nil {
		t.Fatal("Send accepted 101 references; the gateway refuses more than 100")
	}
	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}

	msg.References = msg.References[:100]
	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send refused 100 references: %v", err)
	}
}

func TestSendCarriesAnInlineAttachmentsContentID(t *testing.T) {
	g := accepts(t)
	msg := hello()
	msg.HTML = `<img src="cid:logo">`
	msg.Attachments = []panmail.Attachment{
		{Filename: "logo.png", Content: []byte("png"), ContentID: "logo"},
		{Filename: "receipt.pdf", Content: []byte("%PDF-1.4")},
	}

	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	attachments, _ := g.bodies[0]["attachments"].([]any)
	if len(attachments) != 2 {
		t.Fatalf("attachments = %v, want two", g.bodies[0]["attachments"])
	}
	inline, _ := attachments[0].(map[string]any)
	if inline["contentId"] != "logo" {
		t.Errorf("contentId = %v, want logo", inline["contentId"])
	}
	ordinary, _ := attachments[1].(map[string]any)
	if _, present := ordinary["contentId"]; present {
		t.Error("an ordinary attachment was sent with a content id")
	}
}

func TestSendRejectsAnUnusableContentID(t *testing.T) {
	cases := map[string]string{
		"angle brackets": "<logo>",
		"a space":        "my logo",
		"a newline":      "logo\r\n",
		"not ascii":      "logó",
		"too long":       strings.Repeat("l", 256),
	}

	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			g := accepts(t)
			msg := hello()
			msg.Attachments = []panmail.Attachment{{Filename: "logo.png", Content: []byte("png"), ContentID: id}}

			if _, err := client(t, g).Send(context.Background(), msg); err == nil {
				t.Fatal("Send accepted a content id the gateway refuses")
			}
			if g.calls != 0 {
				t.Errorf("the gateway was called %d times, want 0", g.calls)
			}
		})
	}
}
