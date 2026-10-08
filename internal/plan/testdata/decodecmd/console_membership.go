// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

const privateConsoleProcesses = 2

func privateConsoleMembership(processes []uint32, count uintptr, target, self uint32) bool {
	if count != privateConsoleProcesses || len(processes) != privateConsoleProcesses || target == self {
		return false
	}

	return (processes[0] == target && processes[1] == self) || (processes[1] == target && processes[0] == self)
}
