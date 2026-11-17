// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// ResolveDeclaration applies native path grammar to an owned validated declaration.
// A divider's font is declared in the enclosing scope, before its group's dir.
func ResolveDeclaration(d packet.Declaration, base string) (packet.Declaration, []diag.Diagnostic) {
	owned, ds := packet.ValidateDeclaration(d)
	if len(ds) > 0 {
		return packet.Declaration{}, ds
	}
	d = owned
	resolve := func(base, path, ref string) string {
		if path == "" {
			return ""
		}
		p, err := Path(base, path)
		if err != nil {
			ds = append(ds, diag.Report(diag.PathInvalid, diag.TechnicalFailure("input", err.Error()), ref))
		}
		return p
	}
	style := func(s packet.PageStyle, base string) packet.PageStyle {
		s.Text.Font.Value.Path = resolve(base, s.Text.Font.Value.Path, s.Text.Font.From)
		return s
	}
	d.Output = resolve(base, d.Output, "output")
	d.PageDefaults = style(d.PageDefaults, base)
	if d.Dir != "" {
		base = resolve(base, d.Dir, "dir")
	}
	var walk func([]packet.Item, string)
	walk = func(items []packet.Item, base string) {
		for _, item := range items {
			switch v := item.(type) {
			case *packet.SourceItem:
				v.Path = resolve(base, v.Path, v.Ref)
			case *packet.PageItem:
				v.Style = style(v.Style, base)
			case *packet.GroupItem:
				if v.Divider != nil {
					s := style(*v.Divider, base)
					v.Divider = &s
				}
				scope := base
				if v.Dir != "" {
					scope = resolve(base, v.Dir, v.Ref)
				}
				walk(v.Items, scope)
			case *packet.ContentsItem:
			}
		}
	}
	walk(d.Items, base)
	return d, ds
}
