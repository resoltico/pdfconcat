// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <unistd.h>
#ifdef __APPLE__
#include <spawn.h>
#else
#include <dirent.h>
#include <linux/capability.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <sys/xattr.h>
#endif

static void refuse(const char *operation) {
    fprintf(stderr, "descriptor launcher: %s: errno=%d\n", operation, errno);
    exit(125);
}

static void require_unprivileged(int executable) {
    if (getuid() == 0 || getuid() != geteuid() || getgid() != getegid()) {
        errno = EPERM;
        refuse("unprivileged identity required");
    }
#ifdef __APPLE__
    (void)executable;
    if (issetugid()) { errno = EPERM; refuse("set-id process refused"); }
#else
    uid_t real_uid, effective_uid, saved_uid;
    gid_t real_gid, effective_gid, saved_gid;
    if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0 || getresgid(&real_gid, &effective_gid, &saved_gid) != 0)
        refuse("read saved privilege identity");
    if (real_uid != effective_uid || real_uid != saved_uid || real_gid != effective_gid || real_gid != saved_gid) {
        errno = EPERM;
        refuse("saved privilege identity refused");
    }
    struct __user_cap_header_struct header = {_LINUX_CAPABILITY_VERSION_3, 0};
    struct __user_cap_data_struct data[2];
    memset(data, 0, sizeof(data));
    if (syscall(SYS_capget, &header, data) != 0) refuse("read process capabilities");
    for (size_t i = 0; i < 2; i++) {
        if (data[i].effective || data[i].permitted || data[i].inheritable) {
            errno = EPERM;
            refuse("process capabilities refused");
        }
    }
    for (unsigned capability = 0; capability < 64; capability++) {
        errno = 0;
        int ambient = prctl(PR_CAP_AMBIENT, PR_CAP_AMBIENT_IS_SET, capability, 0, 0);
        if (ambient == -1 && errno == EINVAL && capability > 0) break;
        if (ambient != 0) { errno = EPERM; refuse("ambient capability or query failure refused"); }
    }
    errno = 0;
    if (fgetxattr(executable, "security.capability", NULL, 0) >= 0 || errno != ENODATA)
        refuse("executable file capabilities refused");
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0) refuse("set no-new-privileges");
    if (prctl(PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0) != 1) refuse("verify no-new-privileges");
#endif
}

static void prepare_exec_hygiene(void) {
    for (int fd = 0; fd <= 3; fd++) {
        if (fcntl(fd, F_GETFD) == -1) refuse("required standard or control descriptor missing");
    }
    if (!(fcntl(3, F_GETFD) & FD_CLOEXEC)) { errno = EPROTO; refuse("control descriptor is not close-on-exec"); }
#ifndef __APPLE__
    if (close_range(4, UINT_MAX, 0) != 0) refuse("close inherited descriptors");
    DIR *directory = opendir("/proc/self/fd");
    if (!directory) refuse("verify surviving descriptors");
    int enumeration = dirfd(directory);
    errno = 0;
    struct dirent *entry;
    int survivors = 0;
    while ((entry = readdir(directory)) != NULL) {
        if (entry->d_name[0] == '.') continue;
        char *end;
        long fd = strtol(entry->d_name, &end, 10);
        if (*end || fd < 0 || fd > INT_MAX) { errno = EPROTO; refuse("invalid surviving descriptor"); }
        if (fd == enumeration) continue;
        if (fd > 3) { errno = EPROTO; refuse("unintended descriptor survived"); }
        survivors++;
    }
    if (errno != 0) refuse("read surviving descriptors");
    if (closedir(directory) != 0) refuse("close descriptor verification directory");
    if (survivors != 4) { errno = EPROTO; refuse("wrong surviving descriptor count"); }
#endif
}

static void write_preparation(const char *record) {
    size_t length = strlen(record);
    long atomic_bytes = fpathconf(3, _PC_PIPE_BUF);
    if (atomic_bytes < 0 || length > (size_t)atomic_bytes) { errno = EPROTO; refuse("preparation exceeds atomic pipe record"); }
    if (write(3, record, length) != (ssize_t)length) refuse("write launch preparation");
}

static void execute_product(char **arguments, const char *record) {
#ifdef __APPLE__
    posix_spawnattr_t attributes;
    posix_spawn_file_actions_t actions;
    int error = posix_spawnattr_init(&attributes);
    if (!error) error = posix_spawn_file_actions_init(&actions);
    short flags = POSIX_SPAWN_SETEXEC | POSIX_SPAWN_CLOEXEC_DEFAULT;
    if (!error) error = posix_spawnattr_setflags(&attributes, flags);
    short observed = 0;
    if (!error) error = posix_spawnattr_getflags(&attributes, &observed);
    if (!error && observed != flags) error = EPROTO;
    for (int fd = 0; !error && fd < 3; fd++) error = posix_spawn_file_actions_addinherit_np(&actions, fd);
    if (error) { errno = error; refuse("configure kernel exec hygiene"); }
    write_preparation(record);
    extern char **environ;
    error = posix_spawn(NULL, arguments[0], &actions, &attributes, arguments, environ);
    int action_error = posix_spawn_file_actions_destroy(&actions);
    int attribute_error = posix_spawnattr_destroy(&attributes);
    errno = error ? error : action_error ? action_error : attribute_error ? attribute_error : EPROTO;
    refuse("same-PID kernel exec failed");
#else
    write_preparation(record);
    execv(arguments[0], arguments);
    refuse("execute reviewed product");
#endif
}

int main(int argc, char **argv) {
    if (argc < 4 || strlen(argv[1]) != 32) { errno = EINVAL; refuse("invalid launch arguments"); }
    for (const char *p = argv[1]; *p; p++) {
        if (!((*p >= '0' && *p <= '9') || (*p >= 'a' && *p <= 'f'))) {
            errno = EINVAL;
            refuse("invalid launch token");
        }
    }
    char *end;
    errno = 0;
    uintmax_t requested = strtoumax(argv[2], &end, 10);
    if (errno || *end || requested != 64) { errno = EINVAL; refuse("unsupported descriptor ceiling"); }
    int executable = open(argv[3], O_RDONLY | O_CLOEXEC);
    if (executable < 0) refuse("open reviewed executable");
    struct stat identity;
    if (fstat(executable, &identity) != 0) refuse("read executable identity");
    if (!S_ISREG(identity.st_mode) || (identity.st_mode & (S_ISUID | S_ISGID))) {
        errno = EPERM;
        refuse("set-id or nonregular executable refused");
    }
    require_unprivileged(executable);
    if (fcntl(3, F_SETFD, FD_CLOEXEC) != 0) refuse("protect launch control descriptor");
    if (close(executable) != 0) refuse("close executable identity descriptor");
    prepare_exec_hygiene();
    struct rlimit bound = {64, 64}, observed;
    if (setrlimit(RLIMIT_NOFILE, &bound) != 0 || getrlimit(RLIMIT_NOFILE, &observed) != 0)
        refuse("set and read descriptor ceiling");
    if (observed.rlim_cur != 64 || observed.rlim_max != 64) { errno = EPROTO; refuse("descriptor ceiling mismatch"); }
    struct rlimit raised = {64, 65};
    errno = 0;
    int raised_result = setrlimit(RLIMIT_NOFILE, &raised);
    int raise_errno = errno;
    if (raised_result != -1 || raise_errno != EPERM) { errno = EPROTO; refuse("hard ceiling can be raised"); }
    if (getrlimit(RLIMIT_NOFILE, &observed) != 0 || observed.rlim_cur != 64 || observed.rlim_max != 64)
        refuse("descriptor ceiling changed");
    char record[1024];
    int record_bytes = snprintf(record, sizeof(record), "{\"token\":\"%s\",\"pid\":%ld,\"uid\":%lu,\"euid\":%lu,\"gid\":%lu,\"egid\":%lu,"
        "\"soft\":%ju,\"hard\":%ju,\"raise_errno\":%d,\"device\":%ju,\"inode\":%ju,\"bytes\":%jd,"
        "\"mode\":%ju,\"control_cloexec\":true,\"hygiene_configured\":true}\n",
        argv[1], (long)getpid(), (unsigned long)getuid(), (unsigned long)geteuid(),
        (unsigned long)getgid(), (unsigned long)getegid(), (uintmax_t)observed.rlim_cur, (uintmax_t)observed.rlim_max, raise_errno,
        (uintmax_t)identity.st_dev, (uintmax_t)identity.st_ino, (intmax_t)identity.st_size,
        (uintmax_t)identity.st_mode);
    if (record_bytes < 0 || (size_t)record_bytes >= sizeof(record)) { errno = EOVERFLOW; refuse("preparation record overflow"); }
    execute_product(argv + 3, record);
}
