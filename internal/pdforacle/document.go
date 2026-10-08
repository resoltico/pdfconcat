// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package pdforacle judges PDF output with programs that share no code with the PDF library under test:
// qpdf's JSON dump and checker for structure, Poppler's pdftotext for page text, and pdfinfo and
// pdftoppm for geometry. It deliberately does not import pdfcpu or the engine.
//
// A Document is the structure of one file; Verify compares it with an Expectation written from the
// inputs (source page markers, link graphs, geometry) and returns the checks that failed. A test that
// should detect corruption is a negative control: it feeds Verify a wrong Expectation and requires
// the matching check to fail.
package pdforacle

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type (
	// Document is the parsed structure of one PDF file.
	Document struct {
		objects map[string]any
		trailer map[string]any

		// Path is the file.
		Path string
		// Version is the header version reported by qpdf, for example "1.7".
		Version string

		tools       Tools
		pages       []string
		pageText    []string
		attachments int
	}

	// qpdfDump is the part of qpdf's JSON output that Load reads.
	qpdfDump struct {
		Attachments map[string]any    `json:"attachments"`
		Pages       []qpdfPage        `json:"pages"`
		QPDF        []json.RawMessage `json:"qpdf"`
	}

	// qpdfFile is a decoded qpdf JSON dump: the whole dump, its header section and its object table.
	qpdfFile struct {
		body   map[string]any
		header qpdfHeader
		dump   qpdfDump
	}

	// qpdfPage is one entry of qpdf's page list.
	qpdfPage struct {
		Object string `json:"object"`
	}

	// qpdfHeader is the first qpdf section: the file header facts.
	qpdfHeader struct {
		PDFVersion string `json:"pdfversion"`
	}
)

const (
	// qpdfWarningsExit is qpdf's exit status for "succeeded with warnings". The dump is complete; Verify
	// reports the warnings through its structure check.
	qpdfWarningsExit = 3

	// qpdfSections is the number of sections in qpdf's "qpdf" array: the header facts and the objects.
	qpdfSections = 2

	// maxReferenceHops bounds how many indirect references deref follows.
	maxReferenceHops = 20

	// maxInheritanceDepth bounds how many page-tree levels effective climbs.
	maxInheritanceDepth = 30
)

var (
	referencePattern = regexp.MustCompile(`^(\d+) 0 R$`)

	errQPDFSections = errors.New("qpdf json has the wrong number of qpdf sections")
)

// Load dumps path with qpdf and extracts its text with pdftotext.
func Load(tools Tools, path string) (*Document, error) {
	file, err := dumpOf(tools, path)
	if err != nil {
		return nil, err
	}

	doc := &Document{
		Path: path, Version: file.header.PDFVersion, tools: tools, objects: map[string]any{},
		attachments: len(file.dump.Attachments),
	}

	for key, value := range file.body {
		if name, isObject := strings.CutPrefix(key, "obj:"); isObject {
			doc.objects[name] = value
		}
	}

	doc.trailer = asMap(asMap(file.body["trailer"])["value"])

	for _, page := range file.dump.Pages {
		doc.pages = append(doc.pages, page.Object)
	}

	text, _, err := run(tools.PDFToText, "-layout", path, "-")
	if err != nil {
		return nil, fmt.Errorf("pdftotext %s: %w", path, err)
	}

	doc.pageText = strings.Split(text, "\f")
	if last := len(doc.pageText) - 1; last >= 0 && strings.TrimSpace(doc.pageText[last]) == "" {
		doc.pageText = doc.pageText[:last]
	}

	return doc, nil
}

// dumpOf runs qpdf's JSON dump of path and decodes its two sections: the header facts and the object table.
func dumpOf(tools Tools, path string) (qpdfFile, error) {
	stdout, stderr, err := run(tools.QPDF, "--json=2", path)
	if err != nil && exitCode(err) != qpdfWarningsExit {
		return qpdfFile{}, fmt.Errorf("qpdf --json=2 %s: %w: %s", path, err, stderr)
	}

	var file qpdfFile

	if err = json.Unmarshal([]byte(stdout), &file.dump); err != nil {
		return qpdfFile{}, fmt.Errorf("parse qpdf json: %w", err)
	}

	if len(file.dump.QPDF) != qpdfSections {
		return qpdfFile{}, fmt.Errorf("%w: %d, want %d", errQPDFSections, len(file.dump.QPDF), qpdfSections)
	}

	if err = json.Unmarshal(file.dump.QPDF[0], &file.header); err != nil {
		return qpdfFile{}, fmt.Errorf("parse qpdf header: %w", err)
	}

	if err = json.Unmarshal(file.dump.QPDF[1], &file.body); err != nil {
		return qpdfFile{}, fmt.Errorf("parse qpdf objects: %w", err)
	}

	return file, nil
}

// asMap returns value as a JSON object, or nil when it is anything else.
func asMap(value any) map[string]any {
	typed, matches := value.(map[string]any)
	if !matches {
		return nil
	}

	return typed
}

// asList returns value as a JSON array, or nil when it is anything else.
func asList(value any) []any {
	typed, matches := value.([]any)
	if !matches {
		return nil
	}

	return typed
}

// asString returns value as a JSON string, or "" when it is anything else.
func asString(value any) string {
	typed, matches := value.(string)
	if !matches {
		return ""
	}

	return typed
}

// asNumber returns value as a JSON number, or 0 when it is anything else.
func asNumber(value any) float64 {
	typed, matches := value.(float64)
	if !matches {
		return 0
	}

	return typed
}

// ObjectCount is the number of objects qpdf reports in the file.
func (d *Document) ObjectCount() int { return len(d.objects) }

// PageCount is the number of pages in qpdf's page list.
func (d *Document) PageCount() int { return len(d.pages) }

// PageObjects returns the object reference of each page in order.
func (d *Document) PageObjects() []string { return slices.Clone(d.pages) }

// Attachments is the number of embedded files qpdf finds.
func (d *Document) Attachments() int { return d.attachments }

// FirstLine returns the first non-blank text line of page number (1-based), or "" when the page has none.
func (d *Document) FirstLine(number int) string {
	if number < 1 || number > len(d.pageText) {
		return ""
	}

	for line := range strings.SplitSeq(d.pageText[number-1], "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// RawTexts extracts without Poppler's physical-layout spacing heuristic. This preserves explicit
// spaces in very small text, which -layout may infer away despite the PDF's correct text mapping.
func (d *Document) RawTexts() ([]string, error) {
	text, _, err := run(d.tools.PDFToText, "-raw", d.Path, "-")
	if err != nil {
		return nil, fmt.Errorf("extract raw text: %w", err)
	}

	pages := strings.Split(text, "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}

	return pages, nil
}

// CatalogKeys returns the names of the catalog entries, such as "/Pages".
func (d *Document) CatalogKeys() []string {
	root := d.catalog()
	keys := make([]string, 0, len(root))

	for key := range root {
		keys = append(keys, key)
	}

	return keys
}

// InfoKeys returns the keys of the trailer's /Info dictionary, such as "/Producer".
func (d *Document) InfoKeys() []string {
	info := d.dict(d.trailer["/Info"])
	keys := make([]string, 0, len(info))

	for key := range info {
		keys = append(keys, key)
	}

	return keys
}

// CatalogVersion is the catalog /Version entry without its slash, or "".
func (d *Document) CatalogVersion() string {
	return strings.TrimPrefix(asString(d.catalog()["/Version"]), "/")
}

// EffectiveVersion is the larger of the header version and the catalog /Version, as major*10+minor.
func (d *Document) EffectiveVersion() int {
	return max(versionNumber(d.Version), versionNumber(d.CatalogVersion()))
}

func versionNumber(text string) int {
	var major, minor int
	if _, err := fmt.Sscanf(text, "%d.%d", &major, &minor); err != nil {
		return 0
	}

	return major*10 + minor
}

func (d *Document) catalog() map[string]any {
	return asMap(d.deref(d.trailer["/Root"]))
}

// reference returns value as the text of an indirect reference such as "5 0 R".
func reference(value any) (string, bool) {
	text, isString := value.(string)

	return text, isString && referencePattern.MatchString(text)
}

func isReference(value any) bool {
	_, isRef := reference(value)

	return isRef
}

// deref follows indirect references to the object's value, or to a stream's dictionary.
func (d *Document) deref(value any) any {
	for range maxReferenceHops {
		text, isRef := reference(value)
		if !isRef {
			return value
		}

		object, found := d.objects[text].(map[string]any)
		if !found {
			return nil
		}

		if inner, isValue := object["value"]; isValue {
			value = inner
		} else if stream, isStream := object["stream"].(map[string]any); isStream {
			value = stream["dict"]
		} else {
			return nil
		}
	}

	return nil
}

func (d *Document) dict(value any) map[string]any {
	return asMap(d.deref(value))
}

func (d *Document) list(value any) []any {
	return asList(d.deref(value))
}

// effective finds key on the page dictionary or, failing that, on its ancestors.
func (d *Document) effective(page map[string]any, key string) any {
	for range maxInheritanceDepth {
		if page == nil {
			return nil
		}

		if value, found := page[key]; found {
			return d.deref(value)
		}

		page = d.dict(page["/Parent"])
	}

	return nil
}
