// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import "runtime"

type sourceVariant struct {
	name string
	env  []string
}

// supportedSourceVariants retains six standalone release variants and explicitly selects the
// Darwin native fault lane. Selection does not claim native execution on another host.
func supportedSourceVariants() []sourceVariant {
	variants := make([]sourceVariant, 0, len(archiveTargets())+2)
	for _, target := range archiveTargets() {
		variants = append(variants, sourceVariant{name: target, env: architectureTargetEnv(target)})
	}

	for _, arch := range []string{"amd64", "arm64"} {
		variants = append(variants, sourceVariant{
			name: "darwin/" + arch + "/" + nativeProgressTag,
			env:  []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=1", readonlyGoFlags + " -tags=" + nativeProgressTag},
		})
	}

	return variants
}

// architectureAnalysisVariants keeps CGO0 cross analysis and adds only the supported native
// compiler lane; source discovery separately includes both Darwin architectures.
func architectureAnalysisVariants() []sourceVariant {
	variants := supportedSourceVariants()[:len(archiveTargets())]
	if runtime.GOOS == nativeProgressOS {
		variants = append(variants, sourceVariant{name: "darwin/" + runtime.GOARCH + "/" + nativeProgressTag, env: nativeProgressEnv()})
	}

	return variants
}
