// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import "context"

// Execute validates, inspects, composes and optionally publishes a packet.
func Execute(ctx context.Context, req Request) Result {
	if !req.valid() {
		return invalidRequest(req.Operation)
	}
	admitted := admit(ctx, req)
	var inspected *inspection
	var composed *composition
	var built *buildOutcome
	if !hasErrors(admitted.diagnostics) {
		inspected = inspect(ctx, *admitted.plan, req.InspectionWorkers)
		admitted.roles.AcceptedSources = inspected.sources
		if !hasErrors(inspected.diagnostics) {
			composed = composePacket(ctx, *admitted.plan, inspected, req.Operation)
			if !hasErrors(composed.diagnostics) && req.Operation == Build {
				out := build(ctx, *admitted.plan, composed, admitted.roles)
				built = &out
			}
		}
	}
	result := project(req, admitted, inspected, composed, built)
	records := recordsFor(admitted, inspected, composed)
	records.appendRequested(&result, req.Detail)
	noteCancellation(ctx, &result)
	result = result.Full()
	finalized := len(result.Diagnostics)
	if !admitted.reportRefused {
		writeReport(&result, admitted.roles, records)
	}
	noteCancellation(ctx, &result)
	if len(result.Diagnostics) != finalized {
		result = result.Full()
	}
	return result
}
