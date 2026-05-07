#define ISH_INTERNAL
#include "ish_bridge.h"
#include "kernel/init.h"
#include "kernel/task.h"
#include "kernel/calls.h"
#include "kernel/fs.h"
#include "fs/fake.h"
#include "fs/devices.h"
#include "misc.h"

#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>

static bool g_ready = false;
static pthread_mutex_t g_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_cond = PTHREAD_COND_INITIALIZER;
static int g_exit_code = 0;
static bool g_command_done = false;

// Pipe state for output capture
static int g_pipe_fds[2] = {-1, -1};
static int g_saved_stdout = -1;
static int g_saved_stderr = -1;

#define MAX_OUTPUT_SIZE (16 * 1024 * 1024) // 16MB max output

// ─── exit hook override ──────────────────────────────────

static void bridge_exit_hook(struct task *task, int code) {
    // Instead of exiting, signal completion
    if (task->parent == NULL) {
        g_exit_code = code;
        g_command_done = true;
        pthread_cond_signal(&g_cond);
    }
}

// ─── init ────────────────────────────────────────────────

int ish_init(const char *rootfs_path) {
    if (g_ready) return 0;

    // iOS sandbox doesn't support IOPOL; case sensitivity handled by fakefs

    // Install our exit hook
    exit_hook = bridge_exit_hook;

    // Mount the fake filesystem
    char root_realpath[MAX_PATH + 1] = "/";
    strcpy(root_realpath, rootfs_path);
    strcat(root_realpath, "/data");

    int err = mount_root(&fakefs, root_realpath);
    if (err < 0) {
        fprintf(stderr, "ish_bridge: mount_root failed: %d\n", err);
        return err;
    }

    // Become the first process
    err = become_first_process();
    if (err < 0) {
        fprintf(stderr, "ish_bridge: become_first_process failed: %d\n", err);
        return err;
    }
    current->thread = pthread_self();

    // Set up working directory
    struct fd *pwd = generic_open("/", O_RDONLY_, 0);
    if (IS_ERR(pwd)) {
        fprintf(stderr, "ish_bridge: open / failed: %ld\n", PTR_ERR(pwd));
    } else {
        fs_chdir(current->fs, pwd);
    }

    // Create a basic set of device nodes
    // create_some_device_nodes();

    g_ready = true;
    return 0;
}

bool ish_is_ready(void) {
    return g_ready;
}

// ─── exec ────────────────────────────────────────────────

int ish_exec(const char *command, char **output, size_t *output_len) {
    if (!g_ready) return -1;

    // Create pipes for capturing output
    if (pipe(g_pipe_fds) < 0) {
        *output = strdup("failed to create pipe");
        *output_len = strlen(*output);
        return -1;
    }

    // Save original stdout/stderr
    g_saved_stdout = dup(STDOUT_FILENO);
    g_saved_stderr = dup(STDERR_FILENO);

    // Redirect to pipe
    dup2(g_pipe_fds[1], STDOUT_FILENO);
    dup2(g_pipe_fds[1], STDERR_FILENO);

    g_command_done = false;
    g_exit_code = 0;

    // Build argv for /bin/sh -c "command"
    char argv_buf[4096];
    const char *prog = "/bin/sh";
    strcpy(argv_buf, prog);
    strcpy(argv_buf + strlen(prog) + 1, "-c");
    strcpy(argv_buf + strlen(prog) + 1 + 3, command);
    argv_buf[strlen(prog) + 1 + 3 + strlen(command)] = '\0';

    // Execute the shell
    int err = do_execve(prog, 3, argv_buf, "\0");
    if (err < 0) {
        // Restore stdout/stderr immediately on error
        dup2(g_saved_stdout, STDOUT_FILENO);
        dup2(g_saved_stderr, STDERR_FILENO);
        close(g_saved_stdout);
        close(g_saved_stderr);
        close(g_pipe_fds[0]);
        close(g_pipe_fds[1]);
        *output = strdup("exec failed");
        *output_len = strlen(*output);
        return err;
    }

    // Wait for command to complete (runs in CPU emulation loop)
    // The exit_hook will signal g_cond when the process exits
    pthread_mutex_lock(&g_lock);
    while (!g_command_done) {
        // Run the CPU emulation for one timeslice
        task_run_current();
        pthread_cond_wait(&g_cond, &g_lock);
    }
    pthread_mutex_unlock(&g_lock);

    // Flush and read output from pipe
    fflush(stdout);
    fflush(stderr);

    // Restore stdout/stderr
    dup2(g_saved_stdout, STDOUT_FILENO);
    dup2(g_saved_stderr, STDERR_FILENO);
    close(g_saved_stdout);
    close(g_saved_stderr);
    close(g_pipe_fds[1]); // close write end

    // Read from pipe
    *output = malloc(MAX_OUTPUT_SIZE);
    ssize_t n = read(g_pipe_fds[0], *output, MAX_OUTPUT_SIZE - 1);
    close(g_pipe_fds[0]);

    if (n < 0) n = 0;
    (*output)[n] = '\0';
    *output_len = n;

    return g_exit_code;
}

// ─── shutdown ────────────────────────────────────────────

void ish_shutdown(void) {
    g_ready = false;
    // iSH doesn't have a clean shutdown path; just reset state
}
