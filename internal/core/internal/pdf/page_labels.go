// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

func setPageLabels(s *model.Context, spans []packet.NumberSpan) error {
	if len(spans) == 0 {
		return nil
	}
	nums := types.Array{}
	for _, span := range spans {
		label := types.Dict{"P": types.StringLiteral("")}
		if span.Start != nil {
			label["S"] = types.Name("D")
			label["St"] = types.Integer(*span.Start)
		}
		nums = append(nums, types.Integer(span.Output[0]-1), label)
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	cat["PageLabels"] = types.Dict{"Nums": nums}
	return nil
}
