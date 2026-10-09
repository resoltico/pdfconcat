// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

type (
	// Phases records how far each phase got. Counts and ranges are only trustworthy for completed phases.
	Phases struct {
		Instructions       PhaseState `json:"instructions"`
		InputInspection    PhaseState `json:"input_inspection"`
		Layout             PhaseState `json:"layout"`
		OutputVerification PhaseState `json:"output_verification"`
	}

	// Counts holds page totals. A nil count is unknown, which is not zero.
	Counts struct {
		SourcePages    *int64 `json:"source_pages"`
		GeneratedPages *int64 `json:"generated_pages"`
		TotalPages     *int64 `json:"total_pages"`
	}

	// Publication states what happened to the output PDF and to the requested report. A published PDF is
	// never described as untouched, whatever happened to the report afterwards.
	Publication struct {
		ReportWrite             string     `json:"report_write,omitempty"`
		ReportTargetObservation string     `json:"report_target_observation,omitempty"`
		OutputDigest            string     `json:"output_digest,omitempty"`
		Output                  string     `json:"output,omitempty"`
		ReportStatus            WriteState `json:"report_status"`
		ReportPath              string     `json:"report_path,omitempty"`
		ReportFrom              string     `json:"report_from,omitempty"`
		// RecoveryReport names retained recovery data after the PDF commit. RecoveryState says
		// whether its publication metadata is current, still planned, or its owned identity was lost.
		RecoveryReport string        `json:"recovery_report,omitempty"`
		RecoveryState  RecoveryState `json:"recovery_state,omitempty"`
		Published      bool          `json:"published"`
	}

	// Position is a place in a document. Offset is the absolute byte offset including a leading byte order
	// mark; Line and Column are 1-based and Column counts bytes. A command line has no offset, line, or column.
	Position struct {
		Offset *int64 `json:"offset,omitempty"`
		File   string `json:"file"`
		Line   int    `json:"line,omitzero"`
		Column int    `json:"column,omitzero"`
	}

	// Location is a Position with the JSON pointer of the node, or the zero-based command-line operand. It repeats
	// the Position fields rather than embedding Position, which keeps the pointer-bearing fields together.
	Location struct {
		ArgvIndex *int   `json:"argv_index,omitempty"`
		Offset    *int64 `json:"offset,omitempty"`
		File      string `json:"file"`
		Pointer   string `json:"pointer,omitempty"`
		Line      int    `json:"line,omitzero"`
		Column    int    `json:"column,omitzero"`
	}

	// Diagnostic is one discovered problem. Codes are stable; messages are not.
	Diagnostic struct {
		Recovery           *Recovery `json:"recovery,omitempty"`
		Location           *Location `json:"location,omitempty"`
		Severity           Severity  `json:"severity"`
		ConsequenceContext string    `json:"consequence_context,omitempty"`
		Stage              Stage     `json:"stage"`
		Code               Code      `json:"code"`
		Path               string    `json:"path,omitempty"`
		Message            string    `json:"message"`
		Cause              string    `json:"cause,omitempty"`
		Consumers          []string  `json:"consumers,omitempty"`
	}

	// PageRange is the inclusive 1-based range of output pages of a part.
	PageRange struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}

	// Part is one contribution to the output. A blank part with Pages > 1 is a compact run of identical pages.
	Part struct {
		Range *PageRange `json:"range"`
		Pages *int64     `json:"pages"`
		// Source indexes Report.Sources; set for a PDF part.
		Source *int `json:"source,omitempty"`
		// Style indexes Report.Styles; set for a blank part whose appearance was resolved.
		Style  *int     `json:"style,omitempty"`
		ID     string   `json:"id"`
		Kind   PartKind `json:"kind"`
		Origin Position `json:"origin"`
	}

	// Source is the identity of a captured source PDF. Digest is the SHA-256 of the captured bytes.
	Source struct {
		Bytes      *int64          `json:"bytes"`
		Path       string          `json:"path"`
		Digest     string          `json:"digest,omitempty"`
		Geometries []GeometryRange `json:"geometries,omitempty"`
	}

	// Font is the identity of a font used by a resolved style. File is the absolute path of a supplied
	// font file and is absent for the built-in font.
	Font struct {
		Digest string `json:"digest"`
		Name   string `json:"name"`
		File   string `json:"file,omitempty"`
	}

	// PageSize is a resolved canvas or final sheet size in physical points.
	PageSize struct {
		Origin SizeOrigin `json:"origin"`
		Width  float64    `json:"width"`
		Height float64    `json:"height"`
	}

	// Rect is a rectangle in page coordinates (points, y up): bottom-left corner and size.
	Rect struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}

	// TextSettings is the resolved text appearance except the font reference. Bounds is computed geometry;
	// every other value is the resolved configuration.
	TextSettings struct {
		Bounds    *Rect   `json:"bounds"`
		InkBounds *Rect   `json:"ink_bounds"`
		Value     string  `json:"value"`
		Color     string  `json:"color"`
		Anchor    string  `json:"anchor"`
		Align     string  `json:"align"`
		Overflow  string  `json:"overflow"`
		Size      float64 `json:"size"`
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Width     float64 `json:"width"`
		Leading   float64 `json:"leading"`
	}

	// Text is TextSettings with its font referenced by index into Report.Fonts.
	Text struct {
		TextSettings

		Font     int           `json:"font"`
		Findings []TextFinding `json:"findings,omitempty"`
	}

	// TextFinding is a computed overflow finding retained for either overflow policy.
	TextFinding struct {
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
		Line   int    `json:"line"`
	}

	// Style is the resolved authored canvas and appearance, stored once and referenced by parts.
	// Geometry and FinalText distinguish a fitted sheet from the authored Size/Text settings.
	// Background is "none" or "#rrggbb"; Text is absent for a page without text.
	Style struct {
		FinalText  *TextPlacement `json:"final_text,omitempty"`
		Geometry   *int           `json:"geometry,omitempty"`
		Text       *Text          `json:"text,omitempty"`
		Background string         `json:"background"`
		Size       PageSize       `json:"size"`
	}

	// Producer identifies the executable that captured this run; it is provenance, not quality certification.
	Producer struct {
		Tool     string `json:"tool"`
		Version  string `json:"version"`
		Commit   string `json:"commit"`
		Go       string `json:"go"`
		Platform string `json:"platform"`
	}

	// Report is the complete captured result of a build or check. Command errors use CommandError.
	Report struct {
		Fit             *FitDeclaration `json:"fit,omitempty"`
		Geometries      []Geometry      `json:"geometries,omitempty"`
		AttemptID       string          `json:"attempt_id"`
		Producer        *Producer       `json:"producer,omitempty"`
		Counts          Counts          `json:"counts"`
		Kind            string          `json:"kind"`
		Status          Status          `json:"status"`
		Command         string          `json:"command"`
		Phases          Phases          `json:"phases"`
		Publication     Publication     `json:"publication"`
		Diagnostics     []Diagnostic    `json:"diagnostics"`
		Parts           []Part          `json:"parts"`
		Sources         []Source        `json:"sources"`
		Fonts           []Font          `json:"fonts"`
		Styles          []Style         `json:"styles"`
		FormatVersion   int             `json:"format_version"`
		DiagnosticCount int             `json:"diagnostic_count"`
		ErrorCount      int             `json:"error_count"`
		WarningCount    int             `json:"warning_count"`
		// ProgressInterrupted records observed loss of requested live telemetry.
		// Absence is not proof of complete delivery; staged reports can predate it.
		ProgressInterrupted bool `json:"progress_interrupted,omitzero"`
	}
)

const (
	// Version is the shared public JSON format version for responses and complete reports.
	Version = 2

	// KindReport marks a complete report.
	KindReport = "report"
	// KindSummary marks a summary of one.
	KindSummary = "summary"

	// MaxReportBytes bounds a saved report, whether written or read.
	MaxReportBytes = 256 << 20
	// MaxNesting bounds the JSON nesting of a saved report.
	MaxNesting = 64
	// MaxNodes bounds the JSON values of a saved report.
	MaxNodes = 2_000_000

	// MaxTextRunes bounds the text of one style, in Unicode scalar values.
	MaxTextRunes = 10000

	// BackgroundNone is the background of an unpainted page.
	BackgroundNone = "none"

	commandBuild = "build"
	commandCheck = "check"
	// reportCommand is the command that reads a saved report.
	reportCommand = "report"
)
