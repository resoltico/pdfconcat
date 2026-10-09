// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import "github.com/resoltico/pdfconcat/internal/assembly"

func (p *parser) fitTarget() error {
	text, value, err := p.readString("A4 or Legal")
	if err != nil {
		return err
	}

	target, err := assembly.ParseFitTarget(text)
	if err != nil {
		return p.fail(StageShape, CodeBadValue, value.start, "/fit_to", "fit_to needs A4 or Legal, not %q", text)
	}

	p.job.FitTo = assembly.Set(target, p.origin(value))

	return nil
}
