// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import "strings"

// releaseLinkerVersion reads the consumed -X main.version assignment, including last-wins overrides.
// Release builds deliberately allow only stripping flags and string assignments; unknown operands fail closed.
func releaseLinkerVersion(recorded string) (string, bool) {
	args, valid := recordedLinkerArgs(recorded)
	if !valid {
		return "", false
	}

	version, found := "", false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-s" || arg == "-w":
			continue
		case arg == "-X":
			index++
			if index == len(args) {
				return "", false
			}

			arg = args[index]
		case strings.HasPrefix(arg, "-X="):
			arg = strings.TrimPrefix(arg, "-X=")
		default:
			return "", false
		}

		symbol, value, assigned := linkerAssignment(arg)
		if !assigned {
			return "", false
		}

		if symbol == "main.version" {
			version, found = value, true
		}
	}

	return version, found && version != ""
}

// recordedLinkerArgs follows Go cmd/internal/quoted's recorded-argument grammar: outer quotes,
// whitespace separators, no escapes, and no quote processing inside an unquoted field.
func recordedLinkerArgs(recorded string) ([]string, bool) {
	var args []string

	for {
		recorded = strings.TrimLeft(recorded, " \t\n\r")
		if recorded == "" {
			return args, true
		}

		if quote := recorded[0]; quote == '\'' || quote == '"' {
			field, rest, closed := strings.Cut(recorded[1:], string(quote))
			if !closed {
				return nil, false
			}

			args = append(args, field)
			recorded = rest

			continue
		}

		end := strings.IndexAny(recorded, " \t\n\r")
		if end < 0 {
			return append(args, recorded), true
		}

		args = append(args, recorded[:end])
		recorded = recorded[end:]
	}
}

// linkerAssignment uses the linker's importpath.name=value shape without shell interpretation.
func linkerAssignment(arg string) (string, string, bool) {
	symbol, value, assigned := strings.Cut(arg, "=")
	return symbol, value, assigned && strings.Contains(symbol, ".")
}

func releaseVersionProblems(archivePath, version, recorded string) []string {
	if got, valid := releaseLinkerVersion(recorded); !valid || got != version {
		return []string{archivePath + ": linker version does not match archive version"}
	}

	return nil
}
