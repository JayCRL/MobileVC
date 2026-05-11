#ifndef ISH_BRIDGE_H
#define ISH_BRIDGE_H

#include <stdbool.h>
#include <stddef.h>

/// Initialize iSH kernel with the given Alpine fakefs root path.
/// rootfs_path should point to the directory containing "data" and "meta.db".
/// Returns 0 on success, negative on error.
int ish_init(const char *rootfs_path);

/// Check if iSH kernel is initialized and ready.
bool ish_is_ready(void);

/// Execute a shell command inside the Linux environment.
/// command: the shell command to execute (e.g. "apk add nodejs")
/// output: will be allocated and filled with combined stdout+stderr
/// output_len: length of output
/// Returns exit code (0 = success, non-zero = error).
/// Caller must free() the output string.
int ish_exec(const char *command, char **output, size_t *output_len);

/// Shutdown the iSH kernel and free resources.
void ish_shutdown(void);

#endif
