// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	colorChannelMax = 255.0
	numberPrecision = 3
)

// objectCount is the number of indirect objects in a rendered blank, excluding the free head entry.
const objectCount = 5

// Object numbers of a rendered blank; the references inside the dictionaries below must match them.
const (
	objCatalog = iota + 1
	objPages
	objPage
	objContent
	objFont
)

// renderBlank serializes one generated blank as a complete single-page PDF.
func renderBlank(ctx context.Context, spec assembly.BlankSpec) ([]byte, error) {
	lines, err := placeText(ctx, spec)
	if err != nil {
		return nil, err
	}

	content := blankContent(spec, lines)

	var (
		out     bytes.Buffer
		offsets [objectCount + 1]int
	)

	out.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	writeObject := func(number int, body string) {
		offsets[number] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", number, body)
	}

	writeObject(objCatalog, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(objPages, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(objPage, fmt.Sprintf(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		formatNumber(float64(spec.Dim.Width)), formatNumber(float64(spec.Dim.Height))))
	writeObject(objContent, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	writeObject(objFont, fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", spec.Text.Font))

	xrefOffset := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", objectCount+1)

	for number := 1; number <= objectCount; number++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[number])
	}

	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", objectCount+1, xrefOffset)

	return out.Bytes(), nil
}

// blankContent builds the page content stream: optional background fill, then text.
func blankContent(spec assembly.BlankSpec, lines []textLine) string {
	var content strings.Builder

	if background, ok := spec.Background.OrElse(assembly.Color{}), spec.Background.IsSet(); ok {
		fmt.Fprintf(&content, "q\n%s rg\n0 0 %s %s re\nf\nQ\n",
			colorOperands(background), formatNumber(float64(spec.Dim.Width)), formatNumber(float64(spec.Dim.Height)))
	}

	if len(lines) > 0 {
		fmt.Fprintf(&content, "BT\n/F1 %s Tf\n%s rg\n", formatNumber(float64(spec.Text.Size)), colorOperands(spec.Text.Color))

		for _, line := range lines {
			fmt.Fprintf(&content, "%s Tw\n1 0 0 1 %s %s Tm\n(%s) Tj\n",
				formatNumber(line.wordSpacing), formatNumber(line.x), formatNumber(line.baseline), escapeString(line.bytes))
		}

		content.WriteString("ET\n")
	}

	return content.String()
}

func colorOperands(color assembly.Color) string {
	return strings.Join([]string{
		formatNumber(float64(color.R) / colorChannelMax),
		formatNumber(float64(color.G) / colorChannelMax),
		formatNumber(float64(color.B) / colorChannelMax),
	}, " ")
}

// formatNumber renders a PDF real number without exponent or trailing zeros.
func formatNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', numberPrecision, 64)

	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}

	return text
}

// escapeString escapes bytes for a PDF literal string; bytes outside printable
// ASCII are written as octal escapes so the stream stays 7-bit clean.
func escapeString(encoded []byte) string {
	var escaped strings.Builder

	for _, b := range encoded {
		switch {
		case b == '\\' || b == '(' || b == ')':
			escaped.WriteByte('\\')
			escaped.WriteByte(b)
		case b < asciiFirstPrintable || b >= asciiDelete:
			fmt.Fprintf(&escaped, "\\%03o", b)
		default:
			escaped.WriteByte(b)
		}
	}

	return escaped.String()
}
