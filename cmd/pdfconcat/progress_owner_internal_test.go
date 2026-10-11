//go:build darwin || linux || windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
)

func TestProgressOwnerHealthTracksNativeSessionAndWriteFailure(t *testing.T) {
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	t.Parallel()

	t.Run("redirected automatic progress stays healthy without a transport", func(t *testing.T) {
		t.Parallel()
		_, writer := progressPipe(t)
		owner := &progressOwner{file: writer}
		t.Cleanup(owner.release)
		if session := owner.newSession(t.Context(), cli.ProgressAuto, "redirected"); session != nil || owner.transport != nil {
			t.Fatal("redirected automatic mode unexpectedly constructed a presenter")
		}
		if owner.interrupted() {
			t.Fatal("deliberately inactive automatic progress was reported interrupted")
		}
	})

	t.Run("native owner retains transport health after the presenter joins", func(t *testing.T) {
		t.Parallel()
		reader, writer := progressPipe(t)
		owner := &progressOwner{file: writer}
		t.Cleanup(owner.release)
		session := owner.newSession(t.Context(), cli.ProgressJSON, "native-health")
		if session == nil {
			t.Fatal("JSON pipe did not construct a native presenter")
		}
		t.Cleanup(session.Stop)
		if owner.interrupted() {
			t.Fatal("healthy constructed native transport was reported interrupted")
		}
		session.Stop()
		if session.Interrupted() || owner.interrupted() {
			t.Fatal("joining an idle presenter changed native sink health")
		}
		requireProgressNoError(t, reader.Close())
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		// Concurrent fork fixtures can temporarily inherit a reader until exec. A record beyond
		// pipe capacity still cannot complete through such an unread inherited reference.
		if err := owner.WriteRecord(ctx, progressLongRecord(t)); err == nil {
			t.Fatal("native owner accepted a record after its real pipe reader closed")
		}
		if session.Interrupted() || !owner.interrupted() {
			t.Fatal("joined presenter concealed a later real native transport failure")
		}
	})
}
