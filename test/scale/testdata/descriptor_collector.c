/* SPDX-License-Identifier: MPL-2.0
 * Copyright (c) 2026 Ervins Strauhmanis
 * Test-only Darwin libproc collector. Entries are observed samples, not an
 * atomic inventory or a proven peak; the kernel descriptor ceiling is separate.
 */
#include <errno.h>
#include <limits.h>
#include <libproc.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#if !defined(DESCRIPTOR_INITIAL_SLOTS) || !defined(DESCRIPTOR_MAXIMUM_SLOTS)
#error Collector slot limits must be supplied by the owning Go compiler boundary
#endif

static int report(int pid, unsigned count, int native_error, unsigned growth) {
    return printf("p%d\nc%u\ne%d\ng%u\n", pid, count, native_error, growth) < 0 ? 3 : 0;
}

int main(int argc, char **argv) {
    if (argc != 3 || strcmp(argv[1], "-p") != 0 || argv[2][0] == '\0') {
        fputs("descriptor collector requires -p positive-pid\n", stderr);
        return 2;
    }
    for (const char *p = argv[2]; *p; ++p) {
        if (*p < '0' || *p > '9') return 2;
    }
    errno = 0;
    char *end;
    long selected = strtol(argv[2], &end, 10);
    int parse_error = errno;
    if (parse_error != 0 || *end || selected <= 0 || selected > INT_MAX) return 2;
    int pid = (int)selected;
    unsigned slots = DESCRIPTOR_INITIAL_SLOTS;
    unsigned growth = 0;
    for (;;) {
        size_t bytes = slots * sizeof(struct proc_fdinfo);
        struct proc_fdinfo *entries = malloc(bytes);
        if (!entries) return report(pid, 0, ENOMEM, growth);
        errno = 0;
        int received = proc_pidinfo(pid, PROC_PIDLISTFDS, 0, entries, (int)bytes);
        int native_error = errno;
        free(entries);
        if (native_error != 0) return report(pid, 0, native_error, growth);
        if (received < 0 || (size_t)received > bytes || (size_t)received % sizeof(struct proc_fdinfo) != 0)
            return report(pid, 0, EPROTO, growth);
        if ((size_t)received < bytes)
            return report(pid, (unsigned)((size_t)received / sizeof(struct proc_fdinfo)), 0, growth);
        if (slots == DESCRIPTOR_MAXIMUM_SLOTS) return report(pid, 0, EOVERFLOW, growth);
        slots *= 2;
        ++growth;
    }
}
