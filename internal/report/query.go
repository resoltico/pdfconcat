// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

type (
	// Request selects what to read from a saved report. At most one of Part, Page, and View is set; with
	// none, the result is the summary. Details requires a selection. Offset and Limit page a View only.
	Request struct {
		Part   *string
		Page   *int64
		Offset *int64
		Limit  *int64
		View   string
		// ExpectAttempt optionally guards the saved report identity.
		ExpectAttempt string
		// Details expands the selected records to their full resolved values.
		Details bool
	}

	// TextRenderer is a value that renders itself as human text; its JSON form is its own encoding.
	TextRenderer interface {
		RenderText(w io.Writer) error
	}

	// Response is a query result: its content is a *Summary, *Report, *PartResponse, *PageResponse, or
	// *ViewResponse. It renders as bounded human text, and its JSON form is the content's.
	Response struct {
		content  TextRenderer
		detailed bool
	}

	// TextBrief is a bounded description of generated text.
	TextBrief struct {
		Preview     string `json:"preview"`
		Chars       int    `json:"chars"`
		Truncated   bool   `json:"truncated,omitzero"`
		HasFindings bool   `json:"has_findings"`
	}

	// GeneratedBrief is a bounded description of a generated page.
	GeneratedBrief struct {
		Text       *TextBrief `json:"text,omitempty"`
		Background string     `json:"background"`
		Width      float64    `json:"width"`
		Height     float64    `json:"height"`
	}

	// TextDetail is the text of a style with its font materialized.
	TextDetail struct {
		TextSettings

		Findings []TextFinding `json:"findings,omitempty"`

		Font Font `json:"font"`
	}

	// StyleDetail is a style with its font materialized.
	StyleDetail struct {
		CanvasSize *PageSize      `json:"canvas_size,omitempty"`
		Geometry   *Geometry      `json:"geometry,omitempty"`
		FinalText  *TextPlacement `json:"final_text,omitempty"`
		Text       *TextDetail    `json:"text,omitempty"`
		Background string         `json:"background"`
		Size       PageSize       `json:"size"`
	}

	// PartView is a part as a query shows it. Brief records carry Path (a PDF) or Generated (a blank);
	// detailed records carry the materialized Source or Style instead, so one call is enough.
	PartView struct {
		Fit          *FitDeclaration `json:"fit,omitempty"`
		Geometries   []GeometryEntry `json:"geometries,omitempty"`
		FinalSize    *PageSize       `json:"final_size,omitempty"`
		Range        *PageRange      `json:"range"`
		Pages        *int64          `json:"pages"`
		Generated    *GeneratedBrief `json:"generated,omitempty"`
		Source       *Source         `json:"source,omitempty"`
		Style        *StyleDetail    `json:"style,omitempty"`
		ID           string          `json:"id"`
		Kind         PartKind        `json:"kind"`
		Path         string          `json:"path,omitempty"`
		Origin       Position        `json:"origin"`
		WarningCount int             `json:"warning_count"`
	}

	// PartResponse answers a part query.
	PartResponse struct {
		Kind          string   `json:"kind"`
		Part          PartView `json:"part"`
		FormatVersion int      `json:"format_version"`
	}

	// PageResponse answers a page query: the part covering the page, and the page's 1-based position in it.
	PageResponse struct {
		Geometry      *Geometry `json:"geometry,omitempty"`
		Kind          string    `json:"kind"`
		Part          PartView  `json:"part"`
		Page          int64     `json:"page"`
		PageInPart    int64     `json:"page_in_part"`
		FormatVersion int       `json:"format_version"`
	}

	// ViewResponse is one page of a paged view. NextOffset is null after the last record.
	ViewResponse[T any] struct {
		NextOffset *int64 `json:"next_offset"`
		Kind       string `json:"kind"`
		View       string `json:"view"`
		Records    []T    `json:"records"`
		// OversizedRecord marks a single record larger than the response bound, returned alone.
		OversizedRecord bool  `json:"oversized_record,omitzero"`
		Offset          int64 `json:"offset"`
		Total           int   `json:"total"`
		Returned        int   `json:"returned"`
		FormatVersion   int   `json:"format_version"`
	}
)

const (
	// ViewParts names the paged view of parts.
	ViewParts = "parts"
	// ViewDiagnostics names the paged view of diagnostics.
	ViewDiagnostics = "diagnostics"

	// DefaultLimit is the records of a paged view without an explicit limit.
	DefaultLimit = 20
	// MaxLimit bounds the records of a paged view.
	MaxLimit = 100
	// MaxResponseBytes bounds a paged response, except for one oversized first record.
	MaxResponseBytes = 128 << 10

	textOverflowAllow   = "allow"
	expectAttemptOption = "--expect-attempt"
	kindPart            = "part"
	kindPage            = "page"
	kindView            = "view"
)

// NewResponse wraps a result value as a Response.
func NewResponse(content TextRenderer) Response {
	_, full := content.(*Report)
	return Response{content: content, detailed: full}
}

// ContentOf returns the response's content as a *T, and whether it is one.
func ContentOf[T any](response Response) (*T, bool) {
	content, ok := any(response.content).(*T)

	return content, ok
}

// RenderText writes the content as human text.
func (r Response) RenderText(w io.Writer) error {
	if !r.detailed {
		return r.content.RenderText(w)
	}

	return renderComplete(w, r.content)
}

// MarshalJSONTo writes the content's JSON.
func (r Response) MarshalJSONTo(enc *jsontext.Encoder) error {
	err := json.MarshalEncode(enc, r.content)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	return nil
}

// Query answers a request from a saved report without touching any PDF. The report must be valid: one
// returned by Decode, or one for which Validate succeeds. Errors are *Error values: usage errors for bad
// requests, not-found errors for missing parts and pages.
func (r *Report) Query(req Request) (Response, error) {
	response, err := r.selectResponse(req)
	response.detailed = req.Details

	return response, err
}

func (r *Report) selectResponse(req Request) (Response, error) {
	err := req.Validate()
	if err != nil {
		return Response{}, err
	}

	if req.ExpectAttempt != "" && req.ExpectAttempt != r.AttemptID {
		return Response{}, requestUsage(expectAttemptOption, CodeAttemptMismatch,
			"The saved report belongs to a different attempt; use the report for the expected attempt.")
	}

	switch {
	case req.Part != nil:
		return responseOf(r.part(*req.Part, req.Details))
	case req.Page != nil:
		return responseOf(r.page(*req.Page, req.Details))
	case req.View == ViewParts:
		return responseOf(pageOf(ViewParts, len(r.Parts), req, func(i int) PartView { return r.partView(i, req.Details) }))
	case req.View == ViewDiagnostics:
		return responseOf(pageOf(ViewDiagnostics, len(r.Diagnostics), req, func(i int) DiagnosticView {
			if req.Details {
				return fullDiagnosticView(&r.Diagnostics[i])
			}

			return previewDiagnosticView(&r.Diagnostics[i])
		}))
	default:
		return NewResponse(r.Summary()), nil
	}
}

// responseOf wraps a successful result as a Response and passes a failure through.
func responseOf[T TextRenderer](content T, err error) (Response, error) {
	if err != nil {
		return Response{}, err
	}

	return NewResponse(content), nil
}

// Validate checks request semantics without artifact access. Errors are *Error values; Option names
// the corresponding command-line declaration for adapters that can supply provenance.
func (q Request) Validate() error {
	err := q.checkSelection()
	if err != nil {
		return err
	}

	if q.Page != nil && *q.Page < 1 {
		return requestUsage("--page", CodeInvalidNumber, "--page must be at least 1 (pages are 1-based)")
	}

	if q.ExpectAttempt != "" && !attemptPattern.MatchString(q.ExpectAttempt) {
		return requestUsage(expectAttemptOption, CodeInvalidExpectation, "--expect-attempt must be a 26-character base32 attempt identity")
	}

	return q.checkPaging()
}

func requestUsage(option string, code Code, format string, args ...any) *Error {
	err := usage(code, format, args...)
	err.Option = option

	return err
}

func (q Request) checkSelection() error {
	selectors := 0

	for _, set := range []bool{q.Part != nil, q.Page != nil, q.View != ""} {
		if set {
			selectors++
		}
	}

	switch {
	case selectors > 1:
		return requestUsage("", CodeSelectionConflict, "--part, --page, and --view are mutually exclusive; choose one")
	case q.Details && selectors == 0:
		return requestUsage(
			"--details", CodeDetailsNeedSelect,
			"--details expands the records you select; add --part ID, --page N, or --view parts|diagnostics",
		)
	default:
		return q.checkView()
	}
}

func (q Request) checkView() error {
	switch {
	case (q.Offset != nil || q.Limit != nil) && q.View == "":
		option := "--offset"
		if q.Limit != nil {
			option = "--limit"
		}

		return requestUsage(option, CodePagingNeedsView, "--offset and --limit page a --view; add --view parts or --view diagnostics")
	case q.View != "" && q.View != ViewParts && q.View != ViewDiagnostics:
		return requestUsage("--view", CodeUnknownView, "unknown view %q; use parts or diagnostics", q.View)
	default:
		return nil
	}
}

func (q Request) checkPaging() error {
	switch {
	case q.Offset != nil && *q.Offset < 0:
		return requestUsage("--offset", CodeInvalidPaging, "--offset must not be negative")
	case q.Limit != nil && (*q.Limit < 1 || *q.Limit > MaxLimit):
		return requestUsage("--limit", CodeInvalidPaging, "--limit must be between 1 and %d", MaxLimit)
	default:
		return nil
	}
}

func (r *Report) part(id string, details bool) (*PartResponse, error) {
	index := slices.IndexFunc(r.Parts, func(p Part) bool { return p.ID == id })
	if index < 0 {
		return nil, usage(CodePartNotFound, "no part has id %q; list ids with --view parts", id)
	}

	return &PartResponse{FormatVersion: Version, Kind: kindPart, Part: r.partView(index, details)}, nil
}

func (r *Report) page(page int64, details bool) (*PageResponse, error) {
	if r.Phases.Layout != PhaseComplete {
		return nil, usage(CodeLayoutIncomplete,
			"the layout is incomplete, so output pages are unknown; fix the diagnostics (--view diagnostics) and check again")
	}

	index := slices.IndexFunc(r.Parts, func(p Part) bool { return p.Range.Start <= page && page <= p.Range.End })
	if index < 0 {
		return nil, usage(CodePageOutOfRange, "page %d is outside the output's pages 1 to %d", page, *r.Counts.TotalPages)
	}

	response := &PageResponse{
		FormatVersion: Version, Kind: kindPage, Page: page, PageInPart: page - r.Parts[index].Range.Start + 1,
		Part: r.partView(index, details),
	}
	response.Geometry = r.pageGeometry(&r.Parts[index], int(response.PageInPart))

	return response, nil
}

func (r *Report) partView(index int, details bool) PartView {
	part := &r.Parts[index]

	view := PartView{
		WarningCount: r.relevantCounts([]string{part.ID}).WarningCount,
		ID:           part.ID,
		Kind:         part.Kind,
		Origin:       part.Origin,
		Range:        part.Range,
		Pages:        part.Pages,
	}
	if r.Fit != nil {
		view.Fit = clonePointer(r.Fit)
		view.FinalSize = clonePointer(&r.Fit.Size)
	}

	switch {
	case part.Source != nil && details:
		source := r.Sources[*part.Source]
		view.Source = &source
		view.Geometries = r.sourceGeometryEntries(&source)
	case part.Source != nil:
		view.Path = r.Sources[*part.Source].Path
	case part.Style != nil && details:
		view.Style = r.styleDetail(&r.Styles[*part.Style])
	case part.Style != nil:
		view.Generated = r.generatedBrief(&r.Styles[*part.Style])
	default:
	}

	return view
}

func (r *Report) styleDetail(s *Style) *StyleDetail {
	detail := &StyleDetail{Background: s.Background, Size: s.Size}
	if s.Geometry != nil {
		detail.CanvasSize = clonePointer(&s.Size)
		detail.Size = r.Fit.Size
		detail.Geometry = clonePointer(&r.Geometries[*s.Geometry])
		detail.FinalText = clonePlacement(s.FinalText)
	}

	if s.Text != nil {
		detail.Text = &TextDetail{TextSettings: s.Text.TextSettings, Font: r.Fonts[s.Text.Font], Findings: slices.Clone(s.Text.Findings)}
	}

	return detail
}

func (r *Report) generatedBrief(s *Style) *GeneratedBrief {
	brief := &GeneratedBrief{Background: s.Background, Width: s.Size.Width, Height: s.Size.Height}
	if s.Geometry != nil {
		brief.Width, brief.Height = r.Fit.Size.Width, r.Fit.Size.Height
	}

	if s.Text != nil {
		cut, truncated := preview(s.Text.Value)
		brief.Text = &TextBrief{
			Preview:     cut,
			Chars:       utf8.RuneCountInString(s.Text.Value),
			Truncated:   truncated,
			HasFindings: len(s.Text.Findings) > 0,
		}
	}

	return brief
}

// pageOf returns the records of a view from the request's offset, at most the limit, and stops before the
// response would pass MaxResponseBytes, at a whole-record boundary. A first record that alone passes the
// bound is returned alone and marked, with an offset that advances past it.
func pageOf[T any](view string, total int, req Request, record func(int) T) (*ViewResponse[T], error) {
	offset, limit := int64(0), int64(DefaultLimit)

	if req.Offset != nil {
		offset = *req.Offset
	}

	if req.Limit != nil {
		limit = *req.Limit
	}

	response := &ViewResponse[T]{
		FormatVersion: Version, Kind: kindView, View: view, Total: total, Offset: offset, Records: []T{},
	}

	if offset >= int64(total) {
		return response, nil
	}

	last := int(min(offset+limit, int64(total)))

	for index := int(offset); index < last; index++ {
		response.Records = append(response.Records, record(index))
		response.setPageEnd()

		encode := Encode
		if req.Details {
			encode = EncodePretty
		}

		payload, err := encode(response)
		if err != nil {
			return nil, newError(StatusFailed, StageWrite, CodeWriteFailed, nil, err, "cannot encode record %d: %v", index, err)
		}

		if len(payload)+1 <= MaxResponseBytes {
			continue
		}

		if len(response.Records) == 1 {
			response.OversizedRecord = true
			break
		}

		response.Records = response.Records[:len(response.Records)-1]
		response.setPageEnd()

		break
	}

	return response, nil
}

func (v *ViewResponse[T]) setPageEnd() {
	v.Returned = len(v.Records)
	next := v.Offset + int64(v.Returned)

	v.NextOffset = nil
	if next < int64(v.Total) {
		v.NextOffset = &next
	}
}
