// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type fuzzExitObserver struct{}

const fuzzLinuxStatFields = 3 // state, parent PID and process group follow the command name.

func newFuzzExitObserver() (fuzzExitObserver, error) { return fuzzExitObserver{}, nil }
func (*fuzzExitObserver) attach(_ int) error         { return nil }
func (*fuzzExitObserver) release() error             { return nil }

func (*fuzzExitObserver) exited(pid int) (bool, error) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}

		if err != nil {
			return false, fmt.Errorf("observe unreaped fuzz runner: %w", err)
		}

		return info.Signo != 0, nil
	}
}

func signalFuzzGroup(pid int) error {
	if err := unix.Kill(-pid, unix.SIGKILL); err != nil {
		return fmt.Errorf("signal owned fuzz group: %w", err)
	}

	return nil
}

func fuzzGroupTerminal(pid int, signalErr error) (bool, error) {
	if signalErr != nil && !errors.Is(signalErr, unix.ESRCH) {
		return false, fmt.Errorf("terminate owned fuzz group: %w", signalErr)
	}

	members, err := readFuzzLinuxGroup(pid)
	if err != nil {
		return false, err
	}

	if _, leader := members[strconv.Itoa(pid)]; !leader {
		return false, fmt.Errorf("%w: unreaped fuzz group leader absent", errGate)
	}

	terminal := true

	for _, state := range members {
		stopped, stateErr := fuzzLinuxStateTerminal(state)
		if stateErr != nil {
			return false, stateErr
		}

		terminal = terminal && stopped
	}

	return terminal, nil
}

func readFuzzLinuxGroup(pid int) (map[string]string, error) {
	root, err := os.OpenRoot("/proc")
	if err != nil {
		return nil, fmt.Errorf("open native process inventory: %w", err)
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspect native process inventory: %w", err), root.Close())
	}

	members, scanErr := scanFuzzLinuxGroup(root, entries, pid)

	return members, errors.Join(scanErr, root.Close())
}

func scanFuzzLinuxGroup(root *os.Root, entries []os.DirEntry, pid int) (map[string]string, error) {
	members := map[string]string{}

	for _, entry := range entries {
		if _, parseErr := strconv.Atoi(entry.Name()); parseErr != nil {
			continue
		}

		content, readErr := root.ReadFile(entry.Name() + "/stat")
		if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, unix.ESRCH) {
			continue
		} // Unrelated processes may exit during discovery.

		if readErr != nil {
			return nil, fmt.Errorf("inspect native process state: %w", readErr)
		}

		group, state, parseErr := fuzzLinuxProcessState(content)
		if parseErr != nil {
			return nil, parseErr
		}

		if group == pid {
			members[entry.Name()] = state
		}
	}

	return members, nil
}

func fuzzLinuxStateTerminal(state string) (bool, error) {
	switch state {
	case "Z", "X":
		return true, nil
	case "R", "S", "D", "T", "t", "W", "I":
		return false, nil
	default:
		return false, fmt.Errorf("%w: unknown owned fuzz process state", errGate)
	}
}

func fuzzLinuxProcessState(content []byte) (int, string, error) {
	// stat field2 (comm) may contain spaces and parentheses; fields after its final ')' are unambiguous.
	_, after, found := bytes.CutLast(content, []byte(")"))
	if !found {
		return 0, "", fmt.Errorf("%w: malformed native process stat", errGate)
	}

	fields := strings.Fields(string(after))
	if len(fields) < fuzzLinuxStatFields {
		return 0, "", fmt.Errorf("%w: incomplete native process stat", errGate)
	}

	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, "", fmt.Errorf("parse native process group: %w", err)
	}

	return group, fields[0], nil
}
