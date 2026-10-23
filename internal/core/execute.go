// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

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
		inspected = inspect(ctx, *admitted.plan)
		if !hasErrors(inspected.diagnostics) {
			composed = composePacket(ctx, *admitted.plan, inspected, req.Operation)
			if !hasErrors(composed.diagnostics) && req.Operation == Build {
				out := build(ctx, *admitted.plan, composed, req.Overwrite)
				built = &out
			}
		}
	}
	result := project(req, admitted, inspected, composed, built)
	noteCancellation(ctx, &result)
	if !admitted.reportRefused {
		writeReport(&result, admitted.roles)
	}
	noteCancellation(ctx, &result)
	return result.Full()
}
