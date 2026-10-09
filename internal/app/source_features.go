// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
)

// sourceFeatureWarnings projects each captured source fact once, naming every declared occurrence.
func (p *pipeline) sourceFeatureWarnings() {
	consumers := make(map[int][]string)

	for index := range p.files {
		if len(p.files[index].info.Features) > 0 {
			consumers[index] = nil
		}
	}

	if len(consumers) == 0 {
		return
	}

	for _, contribution := range p.flat.Contributions {
		if _, found := consumers[contribution.File]; found && contribution.Kind == assembly.ItemPDF {
			consumers[contribution.File] = append(consumers[contribution.File], p.flat.Source.Pointer(contribution.Origin.Ref, ""))
		}
	}

	for index := range p.files {
		file := &p.files[index]

		use := &p.flat.Files[index]
		for _, feature := range file.info.Features {
			p.builder.AddDiagnostic(index, report.Diagnostic{
				Severity:  report.SeverityWarning,
				Stage:     stageInput,
				Code:      report.Code("source_" + string(feature.Kind) + "_" + string(feature.Disposition)),
				Path:      use.Path,
				Location:  p.at(use.FirstUse, ""),
				Consumers: consumers[index],
				Message:   sourceFeatureMessage(feature),
				Recovery:  &report.Recovery{Action: "edit_input", Location: p.at(use.FirstUse, "")},
			})
		}
	}
}

func sourceFeatureMessage(feature pdfengine.SourceFeature) string {
	switch feature.Kind {
	case pdfengine.FeatureBookmarks:
		return "Source bookmarks are present. Assembly omits the bookmark tree; review this policy before using the packet."
	case pdfengine.FeatureTaggedStructure:
		return "Source tagged structure is present. Assembly omits document tags; " +
			"use another workflow when accessible structure is required."
	case pdfengine.FeatureCatalogAttachments:
		return "A catalog attachment index is present. Assembly omits that index; " +
			"payloads reachable through kept page attachments can remain."
	case pdfengine.FeatureCatalogActions:
		return "Catalog actions are present. Assembly omits catalog actions and JavaScript name trees; " +
			"retained page-local actions are a separate scope."
	case pdfengine.FeaturePageActions:
		return "Executable or consequential page, annotation or field actions are present. Assembly retains them " +
			"without executing or verifying behavior after field names are qualified; review active content before opening output."
	case pdfengine.FeaturePageAttachments:
		return "Page FileAttachment payloads are present. Assembly retains these payloads; removing a catalog index does not sanitize them."
	case pdfengine.FeaturePageLabels:
		return "Source page labels are present. Assembly omits document page-label numbering; review output page references."
	case pdfengine.FeatureOtherCatalogNames:
		return "Other catalog name-tree entries are present. Assembly keeps only destination names; review the omitted catalog features."
	case pdfengine.FeatureURIBase:
		return "Assembly omits catalog URI/Base used by retained relative URI actions; use absolute targets. " +
			"Keeping action bytes does not guarantee external URI resolution."
	default:
		return fmt.Sprintf(
			"Source feature %s has assembly policy %s; review the source and requested packet.",
			feature.Kind,
			feature.Disposition,
		)
	}
}
