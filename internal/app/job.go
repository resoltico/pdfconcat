// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
)

// Source names of plans that are not files.
const (
	stdinName  = "<stdin>"
	inlineName = "<inline>"
)

// resolveCommandPaths makes the --report and -o paths absolute. It is the first step so that a failure at
// any later step can still be saved to the report, and can say which output it concerned.
func (p *pipeline) resolveCommandPaths() error {
	if p.command.ReportPath != "" {
		p.reportPath = p.abs(p.command.ReportPath)
	}

	if p.command.Output != "" {
		p.output = p.abs(p.command.Output)
	}

	return nil
}

// loadJob decodes the job once, from the plan file, standard input, inline JSON or command-line operands.
func (p *pipeline) loadJob(ctx context.Context) error {
	switch p.command.PlanSource {
	case cli.PlanFile:
		return p.loadPlanFile(ctx)
	case cli.PlanStdin:
		return p.decodeInto(ctx, stdinName, p.env.Stdin)
	case cli.PlanInline:
		return p.decodeInto(ctx, inlineName, strings.NewReader(p.command.PlanJSON))
	case cli.PlanOperands:
		return p.compileOperands()
	case cli.PlanNone:
		return p.stopWith(planMissing("give the job as --plan FILE, --plan - (standard input), --plan-json JSON, or PDF operands"))
	default:
		return p.stopWith(planMissing(fmt.Sprintf("plan source %d is not supported", p.command.PlanSource)))
	}
}

// planMissing is the problem of a command that did not give its job in a way the pipeline can read.
func planMissing(message string) problem {
	return problem{
		diagnostic: report.Diagnostic{Stage: report.StageUsage, Code: "plan_missing", Message: message},
		status:     report.StatusInvalid,
	}
}

func (p *pipeline) compileOperands() error {
	job, err := assembly.NewOperandJob(p.env.WorkingDir, p.command.Operands)
	if err != nil {
		return p.stopWith(problem{
			diagnostic: report.Diagnostic{Stage: report.Stage(assembly.StageJob), Code: codeJobInvalid, Message: err.Error()},
			status:     report.StatusInvalid,
		})
	}

	p.job = job

	return nil
}

func (p *pipeline) loadPlanFile(ctx context.Context) error {
	path := p.abs(p.command.PlanPath)

	_, err := p.registry.Add(capture.RolePlan, path)
	if err != nil {
		return p.stopWith(planFileProblem(path, err))
	}

	file, err := openInput(ctx, path)
	if err != nil {
		return p.stopWith(planFileProblem(path, err))
	}

	return p.decodePlanFile(ctx, path, file)
}

// decodePlanFile decodes the plan from file, which it closes. A plan that decoded but whose file then
// failed to close is reported as an unreadable file, because the bytes read may not be the bytes stored.
func (p *pipeline) decodePlanFile(ctx context.Context, path string, file io.ReadCloser) error {
	job, err := plan.Decode(ctx, plan.Input{Name: path, BaseDir: filepath.Dir(path)}, file)
	closeErr := file.Close()

	if err != nil {
		return p.stopWith(planProblem(err))
	}

	if closeErr != nil {
		return p.stopWith(planFileProblem(path, closeErr))
	}

	p.job = job

	return nil
}

// decodeInto decodes a plan that is not a file; its relative paths resolve against --base-dir or the working directory.
func (p *pipeline) decodeInto(ctx context.Context, name string, reader io.Reader) error {
	base, err := p.baseDir()
	if err != nil {
		return err
	}

	job, err := plan.Decode(ctx, plan.Input{Name: name, BaseDir: base}, reader)
	if err != nil {
		return p.stopWith(planProblem(err))
	}

	p.job = job

	return nil
}

// baseDir is the directory a plan without a file resolves against, and must exist.
func (p *pipeline) baseDir() (string, error) {
	if p.command.BaseDir == "" {
		return p.env.WorkingDir, nil
	}

	base := p.abs(p.command.BaseDir)

	info, err := os.Stat(base)
	if err == nil && !info.IsDir() {
		err = errNotDirectory
	}

	if err != nil {
		return "", p.stopWith(problem{
			diagnostic: report.Diagnostic{
				Stage: report.StageUsage, Code: codeBaseDirInvalid, Path: base,
				Message: "--base-dir must name an existing directory: " + err.Error(),
			},
			status: report.StatusInvalid,
		})
	}

	return base, nil
}

// planFileProblem classifies a failure to open the named plan file.
func planFileProblem(path string, err error) problem {
	found := classify(stageInput, err)
	if found.diagnostic.Code == codeSourceUnreadable || found.diagnostic.Code == codeOperationFailed {
		found.diagnostic.Code = codePlanUnreadable
	}

	found.diagnostic.Path = path

	nonregularError, ok := errors.AsType[*capture.NotRegularFileError](err)
	if ok {
		found.diagnostic.Path = nonregularError.Path
		found.diagnostic.Code = codePlanUnreadable
		found.status = report.StatusFailed
	}

	return found
}

// resolveOutput decides the destination: -o (already resolved against the working directory), else the
// plan's output against the plan's base directory.
func (p *pipeline) resolveOutput() error {
	switch {
	case p.output != "":
	case p.job.Output.IsSet():
		path, err := assembly.ResolvePath(p.job.Base, "output", p.job.Output.Value)
		if err != nil {
			return p.stopWith(problem{
				diagnostic: report.Diagnostic{
					Stage: report.Stage(assembly.StagePath), Code: codePathInvalid, Message: err.Error(),
					Location: report.LocationOf(assembly.Locate(p.job.Source, p.job.Output.Origin, "/output")),
				},
				status: report.StatusInvalid,
			})
		}

		p.output = path
	case p.isBuild():
		return p.stopWith(problem{
			diagnostic: report.Diagnostic{
				Stage: report.StageUsage, Code: codeOutputMissing,
				Message: `a build needs a destination: pass -o FILE or set "output" in the plan`,
			},
			status: report.StatusInvalid,
		})
	default:
	}

	return nil
}

// flatten resolves every path and appearance value of the job.
func (p *pipeline) flatten() error {
	flat, err := assembly.Flatten(p.job)
	if err != nil {
		return p.stopAssembly(report.Stage(assembly.StageJob), err)
	}

	p.flat = flat
	if p.command.FitTo != "" {
		flat.FitTo = assembly.Set(p.command.FitTo, assembly.Origin{})
	}

	p.files = make([]inspectedSource, len(flat.Files))

	return nil
}

// checkDestinations applies the destination policy and registers the output and report files, so a plan,
// font, source, output or report file can never be used in two roles.
func (p *pipeline) checkDestinations() error {
	if p.output != "" {
		if err := p.registry.ProtectOutput(p.output); err != nil {
			return p.stopWith(artifactProblem(err))
		}

		err := publish.CheckDestination(p.output, p.command.Overwrite)
		if err != nil {
			return p.stopWith(problem{
				diagnostic: report.Diagnostic{
					Stage: stageOutput, Code: codeOutputInvalid, Path: p.output,
					Message: err.Error(), Location: p.outputLocation(),
				},
				status: report.StatusInvalid,
			})
		}
	}

	if p.reportPath != "" {
		_, err := p.registry.Add(capture.RoleReport, p.reportPath)
		if err != nil {
			return p.stopWith(artifactProblem(err))
		}
	}

	return nil
}

func (p *pipeline) outputLocation() *report.Location {
	if p.command.Output != "" {
		if location := argumentLocation(p.env, "-o"); location != nil {
			return location
		}

		return argumentLocation(p.env, "--output")
	}

	if p.job != nil && p.job.Output.IsSet() {
		return report.LocationOf(assembly.Locate(p.job.Source, p.job.Output.Origin, "/output"))
	}

	return nil
}

// artifactProblem classifies a failure to register an output or report file. A file that cannot be
// inspected at all, such as one in a directory that does not exist, is an invalid destination.
func artifactProblem(err error) problem {
	found := classify(stageOutput, err)
	if found.diagnostic.Code == codeSourceUnreadable {
		found.diagnostic.Code = codeOutputInvalid
		found.status = report.StatusInvalid
	}

	return found
}
