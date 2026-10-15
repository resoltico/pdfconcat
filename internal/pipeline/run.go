// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/atomicfile"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/inputfile"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/result"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type sourceUse struct {
	path  string
	items []string
}

// Run executes a parsed build/check request. Help/schema/version and argv failures
// never enter this function and therefore cannot write reports.
func Run(c context.Context, command cli.Command, cwd string, stdin io.Reader) result.Result {
	r := result.New(command.Name)
	roles := Files{Overwrite: command.Overwrite}
	var j job.Job
	finishRun := func() result.Result {
		noteCancellation(c, &r)
		writeReport(&r, roles)
		noteCancellation(c, &r)
		return r.Full()
	}
	if command.Report != "" {
		path, err := job.Path(cwd, command.Report)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New("path_invalid", err.Error()))
			return r.Full()
		}
		roles.Report = path
		r.Report = &result.Report{Path: path}
	}
	if command.Output != "" {
		path, err := job.Path(cwd, command.Output)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New("path_invalid", err.Error()))
			return finishRun()
		}
		roles.Output = path
		r.Output = &result.Output{Path: path}
	}
	if err := c.Err(); err != nil {
		r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
		return finishRun()
	}
	if command.Plan != "" {
		base := cwd
		file := "<stdin>"
		var data []byte
		var err error
		if command.Plan == "-" {
			if command.BaseDir != "" {
				base, err = job.Path(cwd, command.BaseDir)
			}
			if err == nil {
				data, err = readStdin(c, stdin)
			}
		} else {
			file, err = job.Path(cwd, command.Plan)
			if err == nil {
				roles.Inputs = append(roles.Inputs, file)
				base = filepath.Dir(file)
				data, err = inputfile.Read(c, file, plan.MaxBytes)
			}
		}
		if err != nil {
			code := diag.Code("plan_unreadable")
			if errors.Is(err, inputfile.ErrTooLarge) {
				code = "plan_too_large"
			}
			d := ioDiagnostic(code, err, file)
			r.Diagnostics = append(r.Diagnostics, d)
			return finishRun()
		}
		var ds []diag.Diagnostic
		j, ds = plan.Decode(data, file, base)
		r.Diagnostics = append(r.Diagnostics, ds...)
		if hasErrors(r.Diagnostics) {
			return finishRun()
		}
	} else {
		j = job.Job{BaseDir: cwd}
		generated := 0
		for _, operand := range command.Operands {
			pointer := fmt.Sprintf("argv:%d", operand.Index)
			if len(j.Parts) >= job.MaxEntries {
				r.Diagnostics = append(r.Diagnostics, diag.New("plan_limit", "More than 100000 entries.", pointer))
				break
			}
			if operand.Blank {
				generated++
				if generated > job.MaxGeneratedPages {
					r.Diagnostics = append(r.Diagnostics, diag.New("plan_limit", "More than 100000 generated pages.", pointer))
					break
				}
				style := job.Defaults()
				j.Parts = append(j.Parts, job.Part{Pointer: pointer, Page: &style, Count: 1})
			} else {
				path, err := job.Path(cwd, operand.Path)
				if err != nil {
					r.Diagnostics = append(r.Diagnostics, diag.New("path_invalid", err.Error(), pointer))
					continue
				}
				j.Parts = append(j.Parts, job.Part{Pointer: pointer, Source: path})
			}
		}
		if hasErrors(r.Diagnostics) {
			return finishRun()
		}
	}
	roles.InputsKnown = true
	if command.Output != "" {
		j.Output = roles.Output
	} else {
		roles.Output = j.Output
	}
	if j.Output != "" {
		r.Output = &result.Output{Path: j.Output}
	}
	if command.FitTo != "" {
		size, _ := job.ParseSize(command.FitTo, false)
		j.FitTo = &size
	}
	r.FitTo = j.FitTo
	var generated int64
	for _, part := range j.Parts {
		if part.Page == nil {
			roles.Inputs = append(roles.Inputs, part.Source)
		} else {
			generated += int64(part.Count)
			if part.Page.Text.Font != "default" {
				roles.Inputs = append(roles.Inputs, part.Page.Text.Font)
			}
		}
		entry := result.Part{Item: part.Pointer, Kind: "source", Path: part.Source, FieldsRenamed: []job.FieldRename{}}
		if part.Page != nil {
			entry.Kind = "page"
			entry.Count = part.Count
			entry.Pages = knownCount(int64(part.Count))
			entry.FieldsRenamed = nil
		}
		r.Parts = append(r.Parts, entry)
	}
	r.Pages = &result.Pages{Generated: knownCount(generated)}
	if command.Name == "build" && j.Output == "" {
		r.Diagnostics = append(r.Diagnostics, diag.New("usage_missing_output", "Give -o FILE or plan output."))
	}
	r.Diagnostics = append(r.Diagnostics, roles.Check()...)
	if hasErrors(r.Diagnostics) {
		return finishRun()
	}
	fonts := loadFonts(c, j, &r)
	if hasErrors(r.Diagnostics) {
		return finishRun()
	}
	uses := sourceUses(j)
	facts := map[string]pdf.SourceFacts{}
	domain := map[string]job.SourceFacts{}
	for _, use := range uses {
		data, err := inputfile.Read(c, use.path, 0)
		if err != nil {
			d := ioDiagnostic("source_unreadable", err, use.path)
			d.Items = use.items
			r.Diagnostics = append(r.Diagnostics, d)
		} else {
			f, ds := pdf.Inspect(c, data)
			for i := range ds {
				ds[i].File = use.path
				ds[i].Items = use.items
			}
			r.Diagnostics = append(r.Diagnostics, ds...)
			facts[use.path] = f
			if !hasErrors(ds) {
				domain[use.path] = f.SourceFacts
			}
		}
		if c.Err() != nil {
			break
		}
	}
	for i, part := range j.Parts {
		if f, ok := facts[part.Source]; part.Page == nil && ok {
			r.Parts[i].SHA256 = fmt.Sprintf("%x", f.SHA256)
			r.Parts[i].Bytes = knownCount(f.Bytes)
			if f.Pages > 0 {
				r.Parts[i].Pages = knownCount(int64(f.Pages))
			}
		}
	}
	if hasErrors(r.Diagnostics) {
		return finishRun()
	}
	layout, ds := job.Resolve(j, domain)
	r.Diagnostics = append(r.Diagnostics, ds...)
	r.Diagnostics = append(r.Diagnostics, resolvePolicies(layout, facts, j.FitTo, command.Name == "check")...)
	if hasErrors(r.Diagnostics) {
		return finishRun()
	}
	r.Pages = &result.Pages{Source: knownCount(layout.SourcePages), Generated: knownCount(layout.GeneratedPages), Total: knownCount(layout.TotalPages)}
	for i, p := range layout.Parts {
		r.Parts[i].OutputPages = &[2]int64{p.FirstPage, p.LastPage}
		if p.Page != nil {
			size := p.Size
			r.Parts[i].Size = &size
		}
	}
	pages := layOut(c, layout, fonts, &r)
	if hasErrors(r.Diagnostics) {
		return finishRun()
	}
	if command.Name == "check" {
		return finishRun()
	}
	prepared, err := typeset.PrepareFonts(placedTexts(pages))
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, diag.New("font_invalid", err.Error()))
		return finishRun()
	}
	if err = c.Err(); err != nil {
		r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
		return finishRun()
	}
	staged, err := atomicfile.Create(j.Output)
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, ioDiagnostic("output_write_failed", err, j.Output))
		return finishRun()
	}
	defer staged.Abort()
	hash := sha256.New()
	counter := &countWriter{Writer: staged}
	assembled, err := pdf.Assemble(c, layout, facts, pages, j.FitTo, prepared, io.MultiWriter(counter, hash))
	r.Diagnostics = append(r.Diagnostics, assembled.Diagnostics...)
	for i, names := range assembled.FieldsRenamed {
		r.Parts[i].FieldsRenamed = names
	}
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, pdf.Diagnostic(err))
		return finishRun()
	}
	if err = pdf.Verify(c, staged.Path(), layout.TotalPages); err != nil {
		r.Diagnostics = append(r.Diagnostics, pdf.Diagnostic(err))
		return finishRun()
	}
	if err = c.Err(); err != nil {
		r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
		return finishRun()
	}
	published, err := staged.Commit(command.Overwrite)
	r.Output.Published = published
	if published {
		r.Output.SHA256 = hex.EncodeToString(hash.Sum(nil))
		r.Output.Bytes = knownCount(counter.n)
	}
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, ioDiagnostic("output_publish_failed", err, j.Output))
	}
	return finishRun()
}

func knownCount(n int64) *int64           { return &n }
func hasErrors(ds []diag.Diagnostic) bool { status, _ := diag.Outcome(ds); return status != "ok" }
func ioDiagnostic(code diag.Code, err error, file string) diag.Diagnostic {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "interrupted"
	}
	d := diag.New(code, err.Error())
	if file != "<stdin>" {
		d.File = file
	}
	return d
}

type countWriter struct {
	io.Writer
	n int64
}

func (w *countWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	w.n += int64(n)
	return n, err
}

func sourceUses(j job.Job) []sourceUse {
	var uses []sourceUse
	index := map[string]int{}
	for _, p := range j.Parts {
		if p.Page == nil {
			i, ok := index[p.Source]
			if !ok {
				i = len(uses)
				index[p.Source] = i
				uses = append(uses, sourceUse{path: p.Source})
			}
			uses[i].items = append(uses[i].items, p.Pointer)
		}
	}
	return uses
}

func readStdin(c context.Context, reader io.Reader) ([]byte, error) {
	closer, ok := reader.(io.Closer)
	if !ok {
		return inputfile.ReadStream(c, reader, plan.MaxBytes)
	}
	type outcome struct {
		data []byte
		err  error
	}
	completed := make(chan outcome, 1)
	go func() {
		data, err := inputfile.ReadStream(c, reader, plan.MaxBytes)
		completed <- outcome{data: data, err: err}
	}()
	select {
	case <-c.Done():
		// Closing a synchronous Windows console read can itself block. Never wait
		// for it on the outcome path; the CLI exits after rendering interruption.
		go func() { _ = closer.Close() }()
		return nil, c.Err()
	case value := <-completed:
		if err := c.Err(); err != nil {
			return nil, err
		}
		return value.data, value.err
	}
}

func writeReport(r *result.Result, roles Files) {
	if r.Report == nil {
		return
	}
	refusals := roles.CheckReport()
	r.Diagnostics = append(r.Diagnostics, refusals...)
	if hasErrors(refusals) {
		return
	}
	full := r.Full()
	report := *r.Report
	report.Written = true
	full.Report = &report
	data, err := json.Marshal(full)
	if err == nil {
		var staged *atomicfile.File
		staged, err = atomicfile.Create(roles.Report)
		if err == nil {
			defer staged.Abort()
			_, err = staged.Write(append(data, '\n'))
			if err == nil {
				r.Report.Written, err = staged.Commit(roles.Overwrite && roles.InputsKnown)
			}
		}
	}
	if err != nil {
		d := diag.New("report_write_failed", err.Error())
		d.File = roles.Report
		r.Diagnostics = append(r.Diagnostics, d)
	}
}

func noteCancellation(c context.Context, r *result.Result) {
	if err := c.Err(); err != nil {
		for _, d := range r.Diagnostics {
			if d.Code == "interrupted" {
				return
			}
		}
		r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
	}
}
