package mailer

import (
	"context"
	"crypto/tls"
	"strconv"
	"strings"
	"time"

	"github.com/samber/oops"
	mail "github.com/xhit/go-simple-mail/v2"
)

const (
	defaultSMTPPort    = 25
	defaultConnTimeout = 10 * time.Second
	defaultSendTimeout = 30 * time.Second
)

// Delivers email through an SMTP server - can interface with Amazon SES, SendGrid, Mailgun, etc. using host + creds
type smtpSender struct {
	host       string
	port       string
	username   string
	password   string
	encryption string
	tls        *tls.Config
}

func (*smtpSender) name() string { return "smtp" }

func newSMTPSender(host, port, username, password, encryption string, tlsConfig *tls.Config) (*smtpSender, error) {
	if host == "" {
		return nil, oops.Errorf("smtp host is required")
	}
	return &smtpSender{
		host:       host,
		port:       port,
		username:   username,
		password:   password,
		encryption: encryption,
		tls:        smtpTLSConfig(tlsConfig, host),
	}, nil
}

func (s *smtpSender) send(ctx context.Context, e Email) error {
	port, err := smtpPort(s.port)
	if err != nil {
		return err
	}

	server := mail.NewSMTPClient()
	server.Host = s.host
	server.Port = port
	server.Encryption = smtpEncryption(s.encryption)
	server.TLSConfig = s.tls
	server.KeepAlive = false
	server.ConnectTimeout, server.SendTimeout = smtpTimeouts(ctx)

	if s.username != "" || s.password != "" {
		server.Username = s.username
		server.Password = s.password
	} else {
		server.Authentication = mail.AuthNone
	}

	client, err := server.Connect()
	if err != nil {
		return oops.Wrapf(err, "connecting to smtp server %s:%d", server.Host, server.Port)
	}

	msg := mail.NewMSG()
	msg.SetFrom(e.formattedFrom()).
		AddTo(e.To...).
		SetSubject(e.Subject)
	if len(e.Cc) > 0 {
		msg.AddCc(e.Cc...)
	}
	if len(e.Bcc) > 0 {
		msg.AddBcc(e.Bcc...)
	}
	if e.ReplyTo != "" {
		msg.SetReplyTo(e.ReplyTo)
	}

	// Add HTML after plain-text fallback
	switch {
	case e.HTML != "" && e.Text != "":
		msg.SetBody(mail.TextPlain, e.Text)
		msg.AddAlternative(mail.TextHTML, e.HTML)
	case e.HTML != "":
		msg.SetBody(mail.TextHTML, e.HTML)
	default:
		msg.SetBody(mail.TextPlain, e.Text)
	}

	for _, a := range e.Attachments {
		if a.ContentID != "" {
			msg.Attach(&mail.File{Data: a.Data, Name: a.ContentID, MimeType: a.MIMEType, Inline: true})
			continue
		}
		msg.AddAttachmentData(a.Data, a.Filename, a.MIMEType)
	}

	if err := msg.GetError(); err != nil {
		return oops.Wrapf(err, "building message")
	}
	if err := msg.Send(client); err != nil {
		return oops.Wrapf(err, "sending message")
	}
	return nil
}

// NOTE: Defaults to STARTTLS for unset/unknown values
func smtpEncryption(enc string) mail.Encryption {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "none", "plain", "insecure":
		return mail.EncryptionNone
	case "ssl", "tls", "ssltls", "ssl/tls", "smtps":
		return mail.EncryptionSSLTLS
	default: // "starttls", "" and anything unrecognized
		return mail.EncryptionSTARTTLS
	}
}

func smtpPort(port string) (int, error) {
	port = strings.TrimSpace(port)
	if port == "" {
		return defaultSMTPPort, nil
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return 0, oops.Wrapf(err, "invalid smtp port %q", port)
	}
	return p, nil
}

// NOTE: Derives connect/send timeouts from the context deadline when there is one, else good default
func smtpTimeouts(ctx context.Context) (connect, send time.Duration) {
	connect, send = defaultConnTimeout, defaultSendTimeout
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 {
			connect, send = remaining, remaining
		}
	}
	return connect, send
}

// Builds TLS configuration for the SMTP connection. A caller-provided config
// is used verbatim. Otherwise a minimal config verifying the server against
// host is returned.
func smtpTLSConfig(tlsConfig *tls.Config, host string) *tls.Config {
	if tlsConfig != nil {
		return tlsConfig
	}
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
}
