/* SPDX-License-Identifier: MPL-2.0
 * Copyright (c) 2026 Ervins Strauhmanis
 * Private single-threaded child: close its inherited descriptors, stop while
 * still alive, then exit when the owning parent resumes it.
 */
#include <signal.h>
#include <unistd.h>

int main(void) {
    int maximum = getdtablesize();
    if (maximum <= 0) return 2;
    for (int descriptor = 0; descriptor < maximum; ++descriptor) {
        (void)close(descriptor);
    }
    if (raise(SIGSTOP) != 0) return 3;
    return 0;
}
