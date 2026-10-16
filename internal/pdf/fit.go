// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/job"
)

func fitContext(c context.Context, s *model.Context, size *job.Size) error {
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	for i, page := range pages {
		_, _, attrs, err := s.PageDict(c, i+1, false)
		if err != nil {
			return err
		}
		// Resize expects a canonical quarter-turn; equivalent rotations such as
		// 450 degrees otherwise lose visible source content in the pinned backend.
		page.dict["Rotate"] = types.Integer((attrs.Rotate%360 + 360) % 360)
	}
	if err := pdfcpu.Resize(c, s, nil, &model.Resize{PageDim: &types.Dim{Width: size.Width, Height: size.Height}, UserDim: true, EnforceOrient: true, DisableContentRotation: true}); err != nil {
		return err
	}
	resized, err := rawPages(c, s)
	if err != nil {
		return err
	}
	for _, page := range resized {
		delete(page.dict, "UserUnit")
	}
	return nil
}
