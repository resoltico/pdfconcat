// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"strings"
	"testing"
)

const releaseHeadingFixture = "## [1.2.3] - 2026-10-07"

func TestReleaseNotesPreserveExactDatedSection(t *testing.T) {
	t.Parallel()

	section := releaseHeadingFixture + "\n\n### Added\n\n- Feature.\n\n```text\n## [1.2.3] - 2020-01-01\n```"
	content := "# Changelog\n\n## [Unreleased]\n\n- Later work.\n\n" + section + "\n\n## [1.2.2] - 2026-10-06\n\n- Previous.\n"

	notes, err := changelogReleaseNotes(content, linkerVersionFixture)
	if err != nil || notes != section {
		t.Fatalf("exact section differs: %q %v", notes, err)
	}
}

func TestReleaseNotesRejectMissingEmptyDuplicateAndMalformedSections(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no release section": "## [Unreleased]\n- Pending.\n",
		"wrongversion":       "## [1x2y3] - 2026-10-07\n- Wrong.\n",
		"prefixversion":      "## [1.2.30] - 2026-10-07\n- Wrong.\n",
		"empty":              releaseHeadingFixture + "\n\n",
		"categoryonly":       releaseHeadingFixture + "\n\n### Added\n",
		"duplicate":          releaseHeadingFixture + "\n- One.\n\n" + releaseHeadingFixture + "\n- Two.\n",
		"missingdate":        "## [1.2.3]\n- Feature.\n",
		"invaliddate":        "## [1.2.3] - 2026-02-30\n- Feature.\n",
		"unclosedfence":      releaseHeadingFixture + "\n- Feature.\n```text\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if notes, err := changelogReleaseNotes(content, linkerVersionFixture); err == nil {
				t.Fatalf("%s accepted: %q", name, notes)
			}
		})
	}
}

func TestReleaseNotesDoNotUseUnreleasedOrFollowingSections(t *testing.T) {
	t.Parallel()

	content := "## [Unreleased]\n- Future.\n\n" + releaseHeadingFixture + "\n\n- Current.\n\n## [1.2.2] - 2026-10-06\n- Previous.\n"

	notes, err := changelogReleaseNotes(content, linkerVersionFixture)
	if err != nil || strings.Contains(notes, "Future") || strings.Contains(notes, "Previous") {
		t.Fatalf("section boundary: %q %v", notes, err)
	}
}
