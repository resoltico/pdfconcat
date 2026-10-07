// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"debug/buildinfo"
	"fmt"
	"runtime"
)

// GremlinsModule is the upstream source module of the reviewed mutation tool.
const GremlinsModule = "github.com/go-gremlins/gremlins"

// VerifyGremlinsBinaryMetadata checks the concrete source-built command, independently of printed identity.
// Source builds have devel module metadata; that is not proof of an official tagged module binary.
func VerifyGremlinsBinaryMetadata(path string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read mutation-tool build metadata: %w", err)
	}

	if info.Main.Path != GremlinsModule || info.Path != GremlinsModule+"/cmd/gremlins" {
		return fmt.Errorf("%w: mutation-tool module or main package identity differs", ErrToolVersions)
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		return fmt.Errorf("%w: mutation-tool target differs from native host", ErrToolVersions)
	}

	return nil
}
