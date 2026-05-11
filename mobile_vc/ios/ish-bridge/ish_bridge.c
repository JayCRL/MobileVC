#define ISH_INTERNAL
#define _GNU_SOURCE
#include "ish_bridge.h"
#include "kernel/init.h"
#include "kernel/task.h"
#include "kernel/calls.h"
#include "kernel/fs.h"
#include "fs/fake.h"
#include "fs/devices.h"
#include "fs/tty.h"
#include "debug.h"
#include "misc.h"

#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>

// Worker thread + capture-tty bridge.
//
// All iSH kernel calls run on a single dedicated pthread (g_worker_thread)
// so that the __thread `current` pointer stays consistent. After init we
// long-run /bin/sh on iSH's CPU thread (spawned by task_start) and inject
// commands by writing into the console tty input buffer. Output that the
// shell writes to the same tty is captured by capture_tty_write, which
// scans for an end-of-command sentinel emitted by `echo __ISH_DONE__:$?`.

#define SENTINEL "__ISH_DONE__:"

static char g_rootfs[1024];
static pthread_t g_worker_thread;
static pthread_mutex_t g_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  g_cmd_cond  = PTHREAD_COND_INITIALIZER;
static pthread_cond_t  g_done_cond = PTHREAD_COND_INITIALIZER;

static volatile bool g_init_started = false;
static volatile bool g_init_ok      = false;
static volatile bool g_init_failed  = false;
static volatile bool g_shell_dead   = false;

static char  *g_pending_cmd = NULL;

static char  *g_capture_buf = NULL;
static size_t g_capture_len = 0;
static size_t g_capture_cap = 0;
static int    g_last_exit   = 0;
static volatile bool g_cmd_done = false;
static volatile bool g_busy     = false;

static struct tty_driver g_capture_driver;
static struct tty *g_capture_ttys[8];

// ── capture tty ops ─────────────────────────────────────────
static int  capture_tty_init(struct tty *tty)    { (void)tty; return 0; }
static int  capture_tty_open(struct tty *tty)    { (void)tty; return 0; }
static int  capture_tty_close(struct tty *tty)   { (void)tty; return 0; }
static void capture_tty_cleanup(struct tty *tty) { (void)tty; }
static int  capture_tty_ioctl(struct tty *tty, int cmd, void *arg) {
    (void)tty; (void)cmd; (void)arg; return 0;
}

static int capture_tty_write(struct tty *tty, const void *buf, size_t len, bool blocking) {
    (void)tty; (void)blocking;
    pthread_mutex_lock(&g_lock);

    // mirror to host stderr for debugging
    fwrite(buf, 1, len, stderr);

    if (g_busy) {
        size_t need = g_capture_len + len + 1;
        if (need > g_capture_cap) {
            size_t nc = g_capture_cap ? g_capture_cap : 4096;
            while (nc < need) nc *= 2;
            char *nb = realloc(g_capture_buf, nc);
            if (nb) {
                g_capture_buf = nb;
                g_capture_cap = nc;
            }
        }
        if (g_capture_buf && g_capture_len + len + 1 <= g_capture_cap) {
            memcpy(g_capture_buf + g_capture_len, buf, len);
            g_capture_len += len;
            g_capture_buf[g_capture_len] = '\0';

            char *m = strstr(g_capture_buf, SENTINEL);
            if (m) {
                int code = atoi(m + strlen(SENTINEL));
                size_t prefix = (size_t)(m - g_capture_buf);
                while (prefix > 0 && (g_capture_buf[prefix - 1] == '\n' ||
                                      g_capture_buf[prefix - 1] == '\r')) {
                    prefix--;
                }
                g_capture_buf[prefix] = '\0';
                g_capture_len = prefix;
                g_last_exit = code;
                g_cmd_done = true;
                pthread_cond_broadcast(&g_done_cond);
            }
        }
    }
    pthread_mutex_unlock(&g_lock);
    return (int)len;
}

static const struct tty_driver_ops capture_tty_ops = {
    .init    = capture_tty_init,
    .open    = capture_tty_open,
    .close   = capture_tty_close,
    .write   = capture_tty_write,
    .ioctl   = capture_tty_ioctl,
    .cleanup = capture_tty_cleanup,
};

// ── die / exit hooks ────────────────────────────────────────
static void bridge_die(const char *msg) {
    fprintf(stderr, "[ish_bridge] PANIC: %s\n", msg ? msg : "(null)");
    pthread_mutex_lock(&g_lock);
    g_init_failed = true;
    g_shell_dead = true;
    g_cmd_done = true;
    g_last_exit = -1;
    pthread_cond_broadcast(&g_done_cond);
    pthread_mutex_unlock(&g_lock);
}

static void bridge_exit_hook(struct task *task, int code) {
    if (task && task->parent == NULL) {
        fprintf(stderr, "[ish_bridge] init/sh exited code=%d\n", code);
        pthread_mutex_lock(&g_lock);
        g_shell_dead = true;
        g_cmd_done = true;
        g_last_exit = code;
        pthread_cond_broadcast(&g_done_cond);
        pthread_cond_broadcast(&g_cmd_cond);
        pthread_mutex_unlock(&g_lock);
    }
}

// ── worker ──────────────────────────────────────────────────
static void *worker_main(void *arg) {
    (void)arg;
#ifdef __APPLE__
    pthread_setname_np("ish-worker");
#endif

    char data_path[1280];
    snprintf(data_path, sizeof(data_path), "%s/data", g_rootfs);

    fprintf(stderr, "[ish_bridge] worker: mount_root(%s)\n", data_path);
    int err = mount_root(&fakefs, data_path);
    if (err < 0) { fprintf(stderr, "[ish_bridge] mount_root err=%d\n", err); goto fail; }

    fprintf(stderr, "[ish_bridge] become_first_process\n");
    err = become_first_process();
    if (err < 0) { fprintf(stderr, "[ish_bridge] become_first_process err=%d\n", err); goto fail; }

    fprintf(stderr, "[ish_bridge] create_some_device_nodes\n");
    create_some_device_nodes();

    fprintf(stderr, "[ish_bridge] mount procfs\n");
    err = do_mount(&procfs, "proc", "/proc", "", 0);
    if (err < 0) fprintf(stderr, "[ish_bridge] mount procfs warn=%d\n", err);

    fprintf(stderr, "[ish_bridge] mount devptsfs\n");
    err = do_mount(&devptsfs, "devpts", "/dev/pts", "", 0);
    if (err < 0) fprintf(stderr, "[ish_bridge] mount devptsfs warn=%d\n", err);

    // Register capture tty driver as the console
    g_capture_driver.ops   = &capture_tty_ops;
    g_capture_driver.major = TTY_CONSOLE_MAJOR;
    g_capture_driver.ttys  = g_capture_ttys;
    g_capture_driver.limit = sizeof(g_capture_ttys) / sizeof(g_capture_ttys[0]);
    tty_drivers[TTY_CONSOLE_MAJOR] = &g_capture_driver;
    set_console_device(TTY_CONSOLE_MAJOR, 1);

    fprintf(stderr, "[ish_bridge] create_stdio /dev/console\n");
    err = create_stdio("/dev/console", TTY_CONSOLE_MAJOR, 1);
    if (err < 0) { fprintf(stderr, "[ish_bridge] create_stdio err=%d\n", err); goto fail; }

    // Tune the console termios: disable echo so injected commands don't
    // appear in our captured output. Keep ICANON so sh reads line-by-line.
    struct tty *console = tty_get(&g_capture_driver, TTY_CONSOLE_MAJOR, 1);
    if (IS_ERR(console)) {
        fprintf(stderr, "[ish_bridge] tty_get console err=%p\n", console);
        goto fail;
    }
    console->termios.lflags &= ~(ECHO_ | ECHOE_ | ECHOK_ | ECHOCTL_ | ECHOKE_);
    console->termios.oflags &= ~(OPOST_ | ONLCR_);

    // argv: "/bin/sh\0" — non-interactive, reads commands from /dev/console
    static const char argv_packed[] =
        "/bin/sh\0";
    static const char envp_packed[] =
        "TERM=dumb\0"
        "PATH=/usr/bin:/bin:/usr/sbin:/sbin\0"
        "HOME=/root\0"
        "PS1=\0"
        "PS2=\0"
        "ENV=\0";

    fprintf(stderr, "[ish_bridge] do_execve /bin/sh\n");
    err = do_execve("/bin/sh", 1, argv_packed, envp_packed);
    if (err < 0) { fprintf(stderr, "[ish_bridge] do_execve err=%d\n", err); goto fail; }

    fprintf(stderr, "[ish_bridge] task_start (sh CPU thread)\n");
    task_start(current);

    pthread_mutex_lock(&g_lock);
    g_init_ok = true;
    pthread_cond_broadcast(&g_done_cond);
    pthread_mutex_unlock(&g_lock);
    fprintf(stderr, "[ish_bridge] init COMPLETE\n");

    // Command loop
    for (;;) {
        pthread_mutex_lock(&g_lock);
        while (!g_pending_cmd && !g_shell_dead) {
            pthread_cond_wait(&g_cmd_cond, &g_lock);
        }
        if (g_shell_dead) { pthread_mutex_unlock(&g_lock); break; }
        char *cmd = g_pending_cmd;
        g_pending_cmd = NULL;
        g_capture_len = 0;
        if (g_capture_buf) g_capture_buf[0] = '\0';
        g_cmd_done = false;
        g_busy = true;
        pthread_mutex_unlock(&g_lock);

        ssize_t wrote = tty_input(console, cmd, strlen(cmd), false);
        if (wrote < 0) {
            fprintf(stderr, "[ish_bridge] tty_input err=%zd\n", wrote);
            pthread_mutex_lock(&g_lock);
            g_last_exit = -1;
            g_cmd_done = true;
            g_busy = false;
            pthread_cond_broadcast(&g_done_cond);
            pthread_mutex_unlock(&g_lock);
        }
        free(cmd);

        pthread_mutex_lock(&g_lock);
        while (!g_cmd_done) pthread_cond_wait(&g_done_cond, &g_lock);
        g_busy = false;
        pthread_cond_broadcast(&g_done_cond);
        pthread_mutex_unlock(&g_lock);
    }

    return NULL;

fail:
    pthread_mutex_lock(&g_lock);
    g_init_failed = true;
    g_cmd_done = true;
    pthread_cond_broadcast(&g_done_cond);
    pthread_mutex_unlock(&g_lock);
    return NULL;
}

// ── public api ──────────────────────────────────────────────
int ish_init(const char *rootfs_path) {
    pthread_mutex_lock(&g_lock);
    if (g_init_ok) { pthread_mutex_unlock(&g_lock); return 0; }
    if (g_init_failed) { pthread_mutex_unlock(&g_lock); return -1; }
    if (g_init_started) {
        while (!g_init_ok && !g_init_failed) {
            pthread_cond_wait(&g_done_cond, &g_lock);
        }
        int r = g_init_ok ? 0 : -1;
        pthread_mutex_unlock(&g_lock);
        return r;
    }
    g_init_started = true;
    if (rootfs_path) {
        strncpy(g_rootfs, rootfs_path, sizeof(g_rootfs) - 1);
        g_rootfs[sizeof(g_rootfs) - 1] = '\0';
    }
    die_handler = bridge_die;
    exit_hook   = bridge_exit_hook;
    pthread_mutex_unlock(&g_lock);

    if (pthread_create(&g_worker_thread, NULL, worker_main, NULL) != 0) {
        return -1;
    }
    pthread_detach(g_worker_thread);

    pthread_mutex_lock(&g_lock);
    while (!g_init_ok && !g_init_failed) {
        pthread_cond_wait(&g_done_cond, &g_lock);
    }
    int r = g_init_ok ? 0 : -1;
    pthread_mutex_unlock(&g_lock);
    return r;
}

bool ish_is_ready(void) {
    return g_init_ok && !g_shell_dead;
}

int ish_exec(const char *command, char **output, size_t *output_len) {
    if (!command) command = "";
    if (!g_init_ok || g_shell_dead) {
        const char *msg = g_shell_dead ? "ish shell exited" : "ish not initialized";
        *output = strdup(msg);
        *output_len = strlen(*output);
        return -1;
    }

    char *wrapped = NULL;
    if (asprintf(&wrapped, "%s\necho %s$?\n", command, SENTINEL) < 0 || !wrapped) {
        *output = strdup("oom");
        *output_len = strlen(*output);
        return -1;
    }

    pthread_mutex_lock(&g_lock);
    while (g_pending_cmd != NULL && !g_shell_dead) {
        pthread_cond_wait(&g_done_cond, &g_lock);
    }
    if (g_shell_dead) {
        pthread_mutex_unlock(&g_lock);
        free(wrapped);
        *output = strdup("ish shell exited");
        *output_len = strlen(*output);
        return -1;
    }
    g_pending_cmd = wrapped;
    pthread_cond_broadcast(&g_cmd_cond);

    while (!g_cmd_done) pthread_cond_wait(&g_done_cond, &g_lock);

    *output_len = g_capture_len;
    *output = malloc(g_capture_len + 1);
    if (*output) {
        if (g_capture_len > 0 && g_capture_buf) {
            memcpy(*output, g_capture_buf, g_capture_len);
        }
        (*output)[g_capture_len] = '\0';
    } else {
        *output_len = 0;
    }
    int code = g_last_exit;
    g_cmd_done = false;
    pthread_cond_broadcast(&g_done_cond);
    pthread_mutex_unlock(&g_lock);
    return code;
}

void ish_shutdown(void) {
    // No-op: kernel runs for the lifetime of the process.
}
