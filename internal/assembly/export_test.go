// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

// FlattenWithStyle exposes flattening under the path rules of any style, so Windows rules run on every host.
func FlattenWithStyle(job *Job, style PathStyle) (*Flattened, error) { return flattenAs(job, style) }

// ResolvePathWithStyle exposes path resolution under the path rules of any style.
func ResolvePathWithStyle(style PathStyle, base, what, name string) (Code, string, error) {
	return resolvePathAs(style, base, what, name)
}
