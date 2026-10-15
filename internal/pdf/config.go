// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// configuration isolates each operation from user configuration and network access.
func configuration() (*model.Configuration, error) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		return nil, err
	}
	conf.Offline = true
	conf.ValidationMode = model.ValidationRelaxed
	conf.CreateBookmarks = true
	conf.MergeBookmarkMode = model.MergeBookmarkModePreserve
	return conf, nil
}
