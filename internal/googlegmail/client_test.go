package googlegmail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"testing"
)

func TestDecodeWebSafeBase64(t *testing.T) {
	t.Parallel()

	want := []byte("attachment body")
	tests := map[string]string{
		"unpadded": base64.RawURLEncoding.EncodeToString(want),
		"padded":   base64.URLEncoding.EncodeToString(want),
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeWebSafeBase64(encoded)
			if err != nil {
				t.Fatalf("decodeWebSafeBase64() error = %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("decodeWebSafeBase64() = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildRawMessageWithAttachment(t *testing.T) {
	t.Parallel()

	raw, err := buildRawMessage(DraftOptions{
		To:      []string{"roni@example.com"},
		Subject: "מסמך בעברית",
		Body:    "Please see the attached file.",
		Attachments: []Attachment{{
			Filename: "מסמך.docx",
			MIMEType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			Data:     []byte("docx bytes"),
		}},
	})
	if err != nil {
		t.Fatalf("buildRawMessage() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw message: %v", err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, params = %v, err = %v", mediaType, params, err)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	if _, err := parts.NextPart(); err != nil {
		t.Fatalf("read body part: %v", err)
	}
	attachment, err := parts.NextPart()
	if err != nil {
		t.Fatalf("read attachment part: %v", err)
	}
	if attachment.FileName() != "מסמך.docx" {
		t.Fatalf("attachment filename = %q", attachment.FileName())
	}
	data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, attachment))
	if err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if string(data) != "docx bytes" {
		t.Fatalf("attachment data = %q", data)
	}
}
