// Package mail sends transactional email over SMTP. Amazon SES and the local
// Mailpit container both speak SMTP, so one implementation serves every
// environment.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"mise.tt/api/internal/platform/config"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	// Headers are extra headers such as List-Unsubscribe.
	Headers map[string]string
}

type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

func New(cfg config.MailConfig) (Mailer, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("MAIL_FROM: %w", err)
	}
	if cfg.Driver == "log" {
		return LogMailer{}, nil
	}
	return &SMTPMailer{cfg: cfg, from: from}, nil
}

// LogMailer records that a message would have been sent. It logs the subject
// only: recipients and bodies (which contain tokens) stay out of the logs.
type LogMailer struct{}

func (LogMailer) Send(ctx context.Context, msg Message) error {
	slog.InfoContext(ctx, "mail (log driver, not sent)", "subject", msg.Subject)
	return nil
}

type SMTPMailer struct {
	cfg  config.MailConfig
	from *mail.Address
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") {
		return fmt.Errorf("mail: header injection attempt rejected")
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mail: invalid recipient: %w", err)
	}
	body, err := m.build(to, msg)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dial: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: handshake: %w", err)
	}
	defer client.Close()

	if m.cfg.StartTLS {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if !m.cfg.StartTLS {
			return fmt.Errorf("mail: refusing to send SMTP credentials without STARTTLS")
		}
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := client.Mail(m.from.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: finish body: %w", err)
	}
	return client.Quit()
}

func (m *SMTPMailer) build(to *mail.Address, msg Message) ([]byte, error) {
	boundaryBytes := make([]byte, 12)
	if _, err := rand.Read(boundaryBytes); err != nil {
		return nil, err
	}
	boundary := "mise-" + hex.EncodeToString(boundaryBytes)
	domain := m.from.Address[strings.LastIndex(m.from.Address, "@")+1:]

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", m.from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", hex.EncodeToString(boundaryBytes), domain))
	header("MIME-Version", "1.0")
	for k, v := range msg.Headers {
		if strings.ContainsAny(k+v, "\r\n") {
			return nil, fmt.Errorf("mail: invalid header %q", k)
		}
		header(k, v)
	}
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	for _, part := range []struct{ contentType, body string }{
		{"text/plain; charset=utf-8", msg.Text},
		{"text/html; charset=utf-8", msg.HTML},
	} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.contentType)
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}
