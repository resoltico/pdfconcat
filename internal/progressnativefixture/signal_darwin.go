// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo

// Package progressnativefixture provides native boundaries for isolated progress tests.
package progressnativefixture

/*
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdlib.h>
#include <string.h>

struct progress_signal_state {
	pthread_t thread;
	struct sigaction saved;
};

static int progress_same_action(const struct sigaction *left, const struct sigaction *right) {
	return left->sa_sigaction == right->sa_sigaction && left->sa_flags == right->sa_flags &&
		memcmp(&left->sa_mask, &right->sa_mask, sizeof left->sa_mask) == 0;
}

static struct progress_signal_state *progress_signal_open(int *result) {
	struct progress_signal_state *state = calloc(1, sizeof *state);
	if (state == NULL) {
		*result = ENOMEM;
		return NULL;
	}
	state->thread = pthread_self();
	sigset_t thread_mask;
	*result = pthread_sigmask(SIG_SETMASK, NULL, &thread_mask);
	if (*result != 0) {
		free(state);
		return NULL;
	}
	int blocked = sigismember(&thread_mask, SIGUSR1);
	if (blocked != 0) {
		*result = blocked < 0 ? errno : EINVAL;
		free(state);
		return NULL;
	}
	if (sigaction(SIGUSR1, NULL, &state->saved) < 0) {
		*result = errno;
		free(state);
		return NULL;
	}
	if ((state->saved.sa_flags & (SA_SIGINFO | SA_ONSTACK | SA_RESTART)) !=
		(SA_SIGINFO | SA_ONSTACK | SA_RESTART)) {
		*result = EINVAL;
		free(state);
		return NULL;
	}
	struct sigaction changed = state->saved;
	changed.sa_flags &= ~SA_RESTART;
	if (sigaction(SIGUSR1, &changed, NULL) < 0) {
		*result = errno;
		free(state);
		return NULL;
	}
	struct sigaction actual;
	if (sigaction(SIGUSR1, NULL, &actual) < 0) {
		*result = errno;
	} else if (!progress_same_action(&changed, &actual)) {
		*result = EINVAL;
	} else {
		*result = 0;
	}
	return state;
}

static int progress_signal_send(struct progress_signal_state *state) {
	return pthread_kill(state->thread, SIGUSR1);
}

static int progress_signal_close(struct progress_signal_state *state) {
	int result = 0;
	if (sigaction(SIGUSR1, &state->saved, NULL) < 0) {
		result = errno;
	} else {
		struct sigaction actual;
		if (sigaction(SIGUSR1, NULL, &actual) < 0) {
			result = errno;
		} else if (!progress_same_action(&state->saved, &actual)) {
			result = EINVAL;
		}
	}
	free(state);
	return result;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"syscall"
)

// SignalState owns a saved disposition and the native identity of the locked caller thread.
// The caller must join all Send calls before Close and keep its OS thread locked until Close.
type SignalState struct {
	state *C.struct_progress_signal_state
}

var errNativeSignalResult = errors.New("invalid native signal result")

// OpenSignalState saves the existing Go SIGUSR1 handler and clears only SA_RESTART.
func OpenSignalState() (*SignalState, error) {
	var result C.int

	state := C.progress_signal_open(&result)
	if err := signalError("configure native non-restarting signal", result); err != nil {
		if state != nil {
			err = errors.Join(err, signalError("restore native signal", C.progress_signal_close(state)))
		}
		return nil, err
	}

	return &SignalState{state: state}, nil
}

// Send delivers SIGUSR1 to the captured live, locked native thread.
func (state *SignalState) Send() error {
	return signalError("signal native writing thread", C.progress_signal_send(state.state))
}

// Close restores and verifies the saved disposition, then frees its native storage.
func (state *SignalState) Close() error {
	if state.state == nil {
		return nil
	}

	result := C.progress_signal_close(state.state)
	state.state = nil

	return signalError("restore native signal", result)
}

func signalError(operation string, result C.int) error {
	if result == 0 {
		return nil
	}
	if result < 0 {
		return fmt.Errorf("%s: %w", operation, errNativeSignalResult)
	}

	return fmt.Errorf("%s: %w", operation, syscall.Errno(result))
}
