package mailer

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"time"

	"github.com/samber/oops"
)

const (
	defaultProductName = "OTPKI"
	defaultGreeting    = "Hello"
	defaultSignature   = "Best regards,"

	defaultButtonColor     = "#2563eb"
	defaultButtonTextColor = "#ffffff"

	logoContentID = "otpki-logo"
)

type Body struct {
	Name       string
	Greeting   string
	Title      string
	Intros     []string
	Dictionary []Entry
	Table      Table
	Actions    []Action
	Outros     []string
	Signature  string
}

type Entry struct {
	Key   string
	Value string
}

type Table struct {
	Headers []string
	Rows    [][]string
}

type Action struct {
	Instructions string
	Button       Button
}

type Button struct {
	Text      string
	Link      string
	Color     string
	TextColor string
}

//go:embed templates/email.html.tmpl
var htmlTemplateSource string

//go:embed templates/email.txt.tmpl
var textTemplateSource string

//go:embed assets/otpki-logo.png
var logoPNG []byte

var (
	htmlTemplate = htmltemplate.Must(htmltemplate.New("email.html").Parse(htmlTemplateSource))
	textTemplate = texttemplate.Must(texttemplate.New("email.txt").Parse(textTemplateSource))
)

type templateData struct {
	Product    string
	LogoSrc    htmltemplate.URL
	Year       int
	Preheader  string
	Heading    string
	Intros     []string
	Dictionary []Entry
	Table      Table
	Actions    []Action
	Outros     []string
	Signature  string
}

func (m *Mailer) SendTemplate(ctx context.Context, to []string, subject string, body Body) error {
	html, text, err := m.render(body)
	if err != nil {
		return err
	}
	return m.Send(ctx, Email{
		To:          to,
		Subject:     subject,
		HTML:        html,
		Text:        text,
		Attachments: []Attachment{logoAttachment()},
	})
}

func logoAttachment() Attachment {
	return Attachment{
		Filename:  "otpki-logo.png",
		MIMEType:  "image/png",
		Data:      logoPNG,
		ContentID: logoContentID,
	}
}

func (m *Mailer) render(body Body) (html, text string, err error) {
	data := newTemplateData(m.product, body)

	var htmlBuf, textBuf bytes.Buffer
	if err := htmlTemplate.Execute(&htmlBuf, data); err != nil {
		return "", "", oops.In(serviceName).Wrapf(err, "rendering html email")
	}
	if err := textTemplate.Execute(&textBuf, data); err != nil {
		return "", "", oops.In(serviceName).Wrapf(err, "rendering text email")
	}
	return htmlBuf.String(), textBuf.String(), nil
}

func newTemplateData(product string, body Body) templateData {
	if product == "" {
		product = defaultProductName
	}

	actions := make([]Action, len(body.Actions))
	for i, a := range body.Actions {
		if a.Button.Color == "" {
			a.Button.Color = defaultButtonColor
		}
		if a.Button.TextColor == "" {
			a.Button.TextColor = defaultButtonTextColor
		}
		actions[i] = a
	}

	heading := resolveHeading(body)
	preheader := heading
	if len(body.Intros) > 0 {
		preheader = body.Intros[0]
	}

	signature := body.Signature
	if signature == "" {
		signature = defaultSignature
	}

	return templateData{
		Product:    product,
		LogoSrc:    htmltemplate.URL("cid:" + logoContentID), //nolint:gosec // Both the CID scheme and logoContentID are internal constants.
		Year:       time.Now().Year(),
		Preheader:  preheader,
		Heading:    heading,
		Intros:     body.Intros,
		Dictionary: body.Dictionary,
		Table:      body.Table,
		Actions:    actions,
		Outros:     body.Outros,
		Signature:  signature,
	}
}

func resolveHeading(body Body) string {
	if body.Title != "" {
		return body.Title
	}
	greeting := body.Greeting
	if greeting == "" {
		greeting = defaultGreeting
	}
	if body.Name != "" {
		return fmt.Sprintf("%s %s,", greeting, body.Name)
	}
	return greeting + ","
}
