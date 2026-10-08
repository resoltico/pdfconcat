// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

// BeforeCommit makes the App run hook after the report is staged and before the PDF is published.
func (a *App) BeforeCommit(hook func()) {
	a.beforeCommit = hook
}

// AfterPDFCommit exposes the test boundary after native PDF commit, before report verification.
func (a *App) AfterPDFCommit(hook func()) { a.afterPDFCommit = hook }
