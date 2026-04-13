package secretform

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/samber/oops"
	"github.com/skip2/go-qrcode"
)

const (
	PWWidth     = 20
	LookupCols  = 4
	LookupWidth = 22
)

//go:embed Menlo-Regular.ttf
var menloRegularTTF []byte

//go:embed Menlo-Bold.ttf
var menloBoldTTF []byte

type Result struct {
	PDF []byte
	KCV string
}

func Generate(secret string) (*Result, error) {
	// NOTE: this salt must stay consistent with other internal KCV tooling for consistency (do not rename to omni trust)
	kcv, err := GetKCVFromPassword(secret, []byte("INTEGRITY Security Services LLC"), 1000000, 6)
	if err != nil {
		return nil, err
	}
	title := "OmniTrust Secret Form"

	year := max(time.Now().Year(), 2024)

	qrPNG, err := qrcode.Encode(secret, qrcode.Highest, 256)
	if err != nil {
		return nil, oops.Errorf("failed to render qr code: %w", err)
	}
	qrWidth := 0.5*float64(len([]rune(secret))) + 25

	// build the main form
	blocks := buildFormBlocks(secret, kcv, PWWidth)

	// render the PDF
	pdfBytes, err := renderPDF(title, year, kcv, blocks, qrPNG, qrWidth)
	if err != nil {
		return nil, err
	}

	return &Result{PDF: pdfBytes, KCV: kcv}, nil
}

func renderPDF(title string, year int, kcv string, blocks formBlocks, qrPNG []byte, qrWidth float64) ([]byte, error) {
	pdf := gofpdf.New("P", "pt", "Letter", "")
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()

	pageW, pageH := pdf.GetPageSize()

	mono := "Courier"
	if m, err := registerMenlo(pdf); err == nil {
		mono = m
	}

	drawBackground(pdf, pageW, pageH, year, mono)
	drawContent(pdf, pageW, title, kcv, blocks, qrPNG, qrWidth, mono)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, oops.Errorf("pdf output error: %w", err)
	}
	return buf.Bytes(), nil
}

func drawBackground(pdf *gofpdf.Fpdf, pageW, pageH float64, year int, mono string) {
	borderX := 39.0
	borderY := 39.0
	borderW := pageW - 90.0
	borderH := pageH - 99.0

	pdf.SetLineWidth(1.0)
	pdf.Rect(borderX, borderY, borderW, borderH, "D")

	footerX := pageW - 545.0
	footerY := pageH - 56.0
	footerW := 480.0
	footer := fmt.Sprintf("\u00A9 2020-%d OmniTrust Security LLC. This document contains proprietary information, including technology that is protected by U.S. Patent Nos. 11,080,413 and 11,580,237. Unauthorized use, reproduction, or distribution is strictly prohibited.", year)

	pdf.SetFont(mono, "", 9)
	pdf.SetXY(footerX, footerY)
	pdf.MultiCell(footerW, 10.8, footer, "", "C", false)
}

func drawContent(pdf *gofpdf.Fpdf, pageW float64, title, kcv string, blocks formBlocks, qrPNG []byte, qrWidth float64, mono string) {
	pageMargin := 40.0

	// header
	headerX := pageMargin + 22.0
	headerY := pageMargin + 19.0
	pdf.SetFont(mono, "B", 26)
	pdf.SetXY(headerX, headerY)
	pdf.CellFormat(0, 28, title, "", 1, "L", false, 0, "")

	// content style
	contentX := pageMargin + 22.0
	contentY := pdf.GetY() + 5.0
	contentW := pageW - contentX - pageMargin
	pdf.SetFont(mono, "", 9)
	pdf.SetXY(contentX, contentY)

	pwLineH := 10.0
	lutLineH := 10.0

	// PW content
	pdf.MultiCell(contentW, pwLineH, blocks.password, "", "L", false)

	// small gap between password block and lookup block (tune 2–6pt)
	pdf.Ln(10)
	pdf.SetX(contentX)

	// Lut content
	pdf.MultiCell(contentW, lutLineH, blocks.lookup, "", "L", false)

	// bottom KCV
	pdf.SetFont(mono, "B", 12)
	pdf.SetXY(55, 707)
	pdf.CellFormat(0, 14, fmt.Sprintf("Key Check Value: %s", kcv), "", 0, "L", false, 0, "")

	// qr
	imgName := "qr"
	pdf.RegisterImageOptionsReader(imgName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(qrPNG))

	qrX := 558.5 - qrWidth
	qrY := 729.0 - qrWidth

	pdf.ImageOptions(imgName, qrX, qrY, qrWidth, qrWidth, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
}

type formBlocks struct {
	password string
	lookup   string
}

func buildFormBlocks(secret, kcv string, pwWidth int) formBlocks {
	if pwWidth <= 0 {
		pwWidth = PWWidth
	}

	var pw strings.Builder
	pw.WriteString(fmt.Sprintf("Key Check Value: %s\n\n", kcv))

	pwRows := chunkByWidth(secret, pwWidth)

	charIndex := 0
	for _, row := range pwRows {
		chars := make([]string, 0, len(row))
		unders := make([]string, 0, len(row))
		inds := make([]string, 0, len(row))

		for _, ch := range []rune(row) {
			chars = append(chars, fmt.Sprintf(" %c ", ch))
			unders = append(unders, "___")
			inds = append(inds, fmt.Sprintf("%03d", charIndex))
			charIndex++
		}
		pw.WriteString(strings.Join(chars, " ") + "\n")
		pw.WriteString(strings.Join(unders, " ") + "\n")
		pw.WriteString(strings.Join(inds, " ") + "\n\n")
	}

	var lut strings.Builder
	lutRows := chunkByWidth(secret, LookupCols)

	charIndex = 0
	for _, row := range lutRows {
		for _, ch := range []rune(row) {
			entry := fmt.Sprintf("%03d: %c %s", charIndex, ch, lookupOrUnknown(ch))
			if len(entry) < LookupWidth {
				entry += strings.Repeat(" ", LookupWidth-len(entry))
			}
			lut.WriteString(entry)
			charIndex++
		}
		lut.WriteString("\n")
	}

	return formBlocks{
		password: pw.String(),
		lookup:   lut.String(),
	}
}

func chunkByWidth(s string, w int) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?s).{1,%d}`, w))
	return re.FindAllString(s, -1)
}

func registerMenlo(pdf *gofpdf.Fpdf) (string, error) {
	pdf.AddUTF8FontFromBytes("Menlo", "", menloRegularTTF)
	pdf.AddUTF8FontFromBytes("Menlo", "B", menloBoldTTF)
	if pdf.Error() != nil {
		return "", pdf.Error()
	}
	return "Menlo", nil
}

func lookupOrUnknown(r rune) string {
	if v, ok := lookup[r]; ok {
		return v
	}
	return "unknown"
}

var lookup = map[rune]string{
	'0': "zero", '1': "one", '2': "two", '3': "three", '4': "four",
	'5': "five", '6': "six", '7': "seven", '8': "eight", '9': "nine",

	'a': "alpha", 'b': "bravo", 'c': "charlie", 'd': "delta", 'e': "echo",
	'f': "foxtrot", 'g': "golf", 'h': "hotel", 'i': "india", 'j': "juliet",
	'k': "kilo", 'l': "lima", 'm': "mike", 'n': "november", 'o': "oscar",
	'p': "papa", 'q': "quebec", 'r': "romeo", 's': "sierra", 't': "tango",
	'u': "uniform", 'v': "victor", 'w': "whiskey", 'x': "xray", 'y': "yankee", 'z': "zulu",

	'A': "ALPHA", 'B': "BRAVO", 'C': "CHARLIE", 'D': "DELTA", 'E': "ECHO",
	'F': "FOXTROT", 'G': "GOLF", 'H': "HOTEL", 'I': "INDIA", 'J': "JULIET",
	'K': "KILO", 'L': "LIMA", 'M': "MIKE", 'N': "NOVEMBER", 'O': "OSCAR",
	'P': "PAPA", 'Q': "QUEBEC", 'R': "ROMEO", 'S': "SIERRA", 'T': "TANGO",
	'U': "UNIFORM", 'V': "VICTOR", 'W': "WHISKEY", 'X': "XRAY", 'Y': "YANKEE", 'Z': "ZULU",

	'~':  "tilde",
	'!':  "exclamation",
	'@':  "at sign",
	'#':  "number sign",
	'$':  "dollar sign",
	'%':  "percentage",
	'^':  "carrat",
	'&':  "ampersand",
	'*':  "asterisk",
	'(':  "open paren",
	')':  "close paren",
	'-':  "minus",
	'+':  "plus",
	'[':  "open bracket",
	']':  "close bracket",
	'{':  "open brace",
	'}':  "close brace",
	':':  "colon",
	';':  "semicolon",
	'"':  "double quote",
	'\'': "single quote",
	'/':  "slash",
	'\\': "backslash",
	'|':  "pipe",
	'?':  "question mark",
	'<':  "less than",
	'>':  "greater than",
	',':  "comma",
	'.':  "period",
	' ':  "space",
}
