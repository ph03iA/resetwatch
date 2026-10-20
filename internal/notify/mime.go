package notify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"time"
)

// Prepare renders the email once: a stable Message-ID from the reminder key and the full MIME message.
// The result is frozen with the reminder, so retries carry identical content. A stable Message-ID does not
// stop Gmail from delivering a retried copy; occasional duplicates are an accepted limitation.
func Prepare(m Message, now time.Time) (Message, error) {
	if err := validateMessage(m); err != nil {
		return Message{}, err
	}
	digest := sha256.Sum256([]byte(m.Key))
	tag := hex.EncodeToString(digest[:16])
	if m.MessageID == "" {
		m.MessageID = "<resetwatch-" + tag + "@resetwatch.invalid>"
	}
	raw, err := renderMIME(m, now, "resetwatch-"+tag)
	if err != nil {
		return Message{}, err
	}
	m.MIME = raw
	return m, nil
}

func renderMIME(m Message, now time.Time, boundary string) (string, error) {
	subject, text, markup := messageContent(m)
	var b bytes.Buffer
	from := mail.Address{Name: "ResetWatch", Address: m.From}
	to := mail.Address{Address: m.To}
	headers := [][2]string{{"From", from.String()}, {"To", to.String()}, {"Subject", mime.QEncoding.Encode("utf-8", subject)},
		{"Date", now.UTC().Format(time.RFC1123Z)}, {"Message-ID", m.MessageID}, {"MIME-Version", "1.0"},
		{"Content-Type", `multipart/alternative; boundary="` + boundary + `"`}}
	for _, header := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", header[0], header[1])
	}
	b.WriteString("\r\n")
	parts := multipart.NewWriter(&b)
	if err := parts.SetBoundary(boundary); err != nil {
		return "", err
	}
	for _, part := range [][2]string{{"text/plain", text}, {"text/html", markup}} {
		if err := writePart(parts, part[0], part[1]); err != nil {
			return "", err
		}
	}
	if err := parts.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writePart(parts *multipart.Writer, contentType, body string) error {
	writer, err := parts.CreatePart(textproto.MIMEHeader{"Content-Type": {contentType + "; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return err
	}
	encoder := quotedprintable.NewWriter(writer)
	if _, err := encoder.Write([]byte(body)); err != nil {
		return err
	}
	return encoder.Close()
}
