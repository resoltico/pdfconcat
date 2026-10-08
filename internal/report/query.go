// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

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
	// none, the result is the summary (the complete report with Details). Offset and Limit page a View only.
	Request struct {
		Part   *string
		Page   *int64
		Offset *int64
		Limit  *int64
		View   string
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
		Preview   string `json:"preview"`
		Chars     int    `json:"chars"`
		Truncated bool   `json:"truncated,omitzero"`
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
		Text       *TextDetail `json:"text,omitempty"`
		Background string      `json:"background"`
		Size       PageSize    `json:"size"`
	}

	// PartView is a part as a query shows it. Brief records carry Path (a PDF) or Generated (a blank);
	// detailed records carry the materialized Source or Style instead, so one call is enough.
	PartView struct {
		Range     *PageRange      `json:"range"`
		Pages     *int64          `json:"pages"`
		Generated *GeneratedBrief `json:"generated,omitempty"`
		Source    *Source         `json:"source,omitempty"`
		Style     *StyleDetail    `json:"style,omitempty"`
		ID        string          `json:"id"`
		Kind      PartKind        `json:"kind"`
		Path      string          `json:"path,omitempty"`
		Origin    Position        `json:"origin"`
	}

	// PartResponse answers a part query.
	PartResponse struct {
		Kind          string   `json:"kind"`
		Part          PartView `json:"part"`
		FormatVersion int      `json:"format_version"`
	}

	// PageResponse answers a page query: the part covering the page, and the page's 1-based position in it.
	PageResponse struct {
		Kind          string   `json:"kind"`
		Part          PartView `json:"part"`
		Page          int64    `json:"page"`
		PageInPart    int64    `json:"page_in_part"`
		FormatVersion int      `json:"format_version"`
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

	kindPart = "part"
	kindPage = "page"
	kindView = "view"
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
	err := req.check()
	if err != nil {
		return Response{}, err
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

// check validates the selection modes and paging values.
func (q Request) check() error {
	err := q.checkSelection()
	if err != nil {
		return err
	}

	return q.checkPaging()
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
		return usage(CodeSelectionConflict, "--part, --page, and --view are mutually exclusive; choose one")
	case q.Details && selectors == 0:
		return usage(
			CodeDetailsNeedSelect,
			"--details expands the records you select; add --part ID, --page N, or --view parts|diagnostics",
		)
	default:
		return q.checkView()
	}
}

func (q Request) checkView() error {
	switch {
	case (q.Offset != nil || q.Limit != nil) && q.View == "":
		return usage(CodePagingNeedsView, "--offset and --limit page a --view; add --view parts or --view diagnostics")
	case q.View != "" && q.View != ViewParts && q.View != ViewDiagnostics:
		return usage(CodeUnknownView, "unknown view %q; use %s or %s", q.View, ViewParts, ViewDiagnostics)
	default:
		return nil
	}
}

func (q Request) checkPaging() error {
	switch {
	case q.Offset != nil && *q.Offset < 0:
		return usage(CodeInvalidPaging, "--offset must not be negative")
	case q.Limit != nil && (*q.Limit < 1 || *q.Limit > MaxLimit):
		return usage(CodeInvalidPaging, "--limit must be between 1 and %d", MaxLimit)
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

	return &PageResponse{
		FormatVersion: Version, Kind: kindPage, Page: page, PageInPart: page - r.Parts[index].Range.Start + 1,
		Part: r.partView(index, details),
	}, nil
}

func (r *Report) partView(index int, details bool) PartView {
	p := &r.Parts[index]
	view := PartView{ID: p.ID, Kind: p.Kind, Origin: p.Origin, Range: p.Range, Pages: p.Pages}

	switch {
	case p.Source != nil && details:
		source := r.Sources[*p.Source]
		view.Source = &source
	case p.Source != nil:
		view.Path = r.Sources[*p.Source].Path
	case p.Style != nil && details:
		view.Style = r.styleDetail(&r.Styles[*p.Style])
	case p.Style != nil:
		view.Generated = generatedBrief(&r.Styles[*p.Style])
	default:
	}

	return view
}

func (r *Report) styleDetail(s *Style) *StyleDetail {
	detail := &StyleDetail{Background: s.Background, Size: s.Size}

	if s.Text != nil {
		detail.Text = &TextDetail{TextSettings: s.Text.TextSettings, Font: r.Fonts[s.Text.Font], Findings: slices.Clone(s.Text.Findings)}
	}

	return detail
}

func generatedBrief(s *Style) *GeneratedBrief {
	brief := &GeneratedBrief{Background: s.Background, Width: s.Size.Width, Height: s.Size.Height}

	if s.Text != nil {
		cut, truncated := preview(s.Text.Value)
		brief.Text = &TextBrief{Preview: cut, Chars: utf8.RuneCountInString(s.Text.Value), Truncated: truncated}
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
