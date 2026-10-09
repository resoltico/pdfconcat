// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

func (p *parser) setProgress(index int, value string) error {
	mode := ProgressMode(value)
	switch mode {
	case ProgressAuto, ProgressJSON, ProgressNone:
		p.cmd.Progress = mode
		return nil
	default:
		return p.fail(CodeInvalidValue, index, "--progress needs auto, json or none, not %q", value)
	}
}
