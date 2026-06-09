package mailer

import (
	"context"
	"crypto/tls"
	"net/mail"

	"github.com/otpki/otpki-pkg/log"
	"github.com/samber/oops"
)

const serviceName = "mailer"

// Provider agnostic outbound format
type Email struct {
	From     string
	FromName string

	To      []string
	Cc      []string
	Bcc     []string
	ReplyTo string

	Subject string
	Text    string
	HTML    string

	Attachments []Attachment
}

type Attachment struct {
	Filename  string
	MIMEType  string
	Data      []byte
	ContentID string // when set, embeds content of file inline
}

type sender interface {
	send(ctx context.Context, e Email) error
	name() string
}

type Mailer struct {
	sender   sender
	product  string
	from     string
	fromName string
	logger   log.Logger
}

// Builds SMTP mailer
func New(logger log.Logger, from, fromName, host, port, username, password, encryption string, tlsConfig *tls.Config) (*Mailer, error) {
	s, err := newSMTPSender(host, port, username, password, encryption, tlsConfig)
	if err != nil {
		return nil, oops.In(serviceName).Wrap(err)
	}
	return &Mailer{
		sender:   s,
		product:  resolveProduct(fromName),
		from:     from,
		fromName: fromName,
		logger:   logger,
	}, nil
}

// Delivers an email - when no sender is configured it logs and returns nil
func (m *Mailer) Send(ctx context.Context, e Email) error {
	if m.sender == nil {
		m.logger.Warn().Ctx(ctx).
			Strs("to", e.To).
			Str("subject", e.Subject).
			Msg(e.Text)
		return nil
	}

	if e.From == "" {
		e.From = m.from
	}
	if e.FromName == "" {
		e.FromName = m.fromName
	}

	if err := e.validate(); err != nil {
		return oops.In(serviceName).Wrap(err)
	}

	if err := m.sender.send(ctx, e); err != nil {
		return oops.In(serviceName).With("provider", m.sender.name()).Wrapf(err, "sending email")
	}
	return nil
}

func (e Email) validate() error {
	switch {
	case e.From == "":
		return oops.Errorf("missing from address")
	case len(e.To) == 0:
		return oops.Errorf("missing recipient")
	case e.Subject == "":
		return oops.Errorf("missing subject")
	case e.Text == "" && e.HTML == "":
		return oops.Errorf("email has no body")
	default:
		return nil
	}
}

// Translates From-header to formatted string
func (e Email) formattedFrom() string {
	if e.FromName == "" {
		return e.From
	}
	return (&mail.Address{Name: e.FromName, Address: e.From}).String()
}

func resolveProduct(fromName string) string {
	if fromName != "" {
		return fromName
	}
	return defaultProductName
}
