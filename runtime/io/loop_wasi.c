/* Event loop backend: wasi poll. The call takes the set of descriptors with
   it, so the backend reads the registered set each wait, and a pipe gives the
   scheduler a way to make the wait look again. A wasm target runs the wait on
   the thread the program runs on, which is why the timeout is a parameter:
   the scheduler passes its own. */

#include "zyxrt_internal.h"

#include <poll.h>
#include <unistd.h>

static int wake_fds[2] = {-1, -1};

int zyxrt_loop_backend_init(void) {
  if (pipe(wake_fds) != 0) return -1;
  return 0;
}

int zyxrt_loop_backend_add(int fd, int events) {
  /* poll reads the set every wait, so the registration is the loop's own
     table. */
  (void)fd;
  (void)events;
  return 0;
}

int zyxrt_loop_backend_wait(int *fds, int *events, int max, int timeout_ms) {
  if (wake_fds[0] < 0) return -1;
  struct pollfd set[ZYXRT_LOOP_BATCH];
  int snap_fds[ZYXRT_LOOP_BATCH];
  int snap_events[ZYXRT_LOOP_BATCH];
  size_t cap = ZYXRT_LOOP_BATCH - 1;
  if (cap > (size_t)max) cap = (size_t)max;
  size_t n = zyxrt_loop_snapshot(snap_fds, snap_events, cap);
  for (size_t i = 0; i < n; i++) {
    set[i].fd = snap_fds[i];
    set[i].events = 0;
    if (snap_events[i] & ZYXRT_FD_READ) set[i].events |= POLLIN;
    if (snap_events[i] & ZYXRT_FD_WRITE) set[i].events |= POLLOUT;
    set[i].revents = 0;
  }
  set[n].fd = wake_fds[0];
  set[n].events = POLLIN;
  set[n].revents = 0;
  n++;

  int got = poll(set, (nfds_t)n, timeout_ms);
  if (got < 0) return -1;
  if (got == 0) return 0;

  int out = 0;
  for (size_t i = 0; i + 1 < n; i++) {
    if (set[i].revents == 0) continue;
    int ev = 0;
    if (set[i].revents & (POLLIN | POLLHUP | POLLERR | POLLNVAL)) ev |= ZYXRT_FD_READ;
    if (set[i].revents & POLLOUT) ev |= ZYXRT_FD_WRITE;
    if (ev == 0) continue;
    fds[out] = snap_fds[i];
    events[out] = ev;
    out++;
  }
  if (set[n - 1].revents != 0) {
    char buf[64];
    ssize_t got_bytes = read(wake_fds[0], buf, sizeof(buf));
    (void)got_bytes;
  }
  return out;
}

void zyxrt_loop_backend_wake(void) {
  if (wake_fds[1] < 0) return;
  char one = 1;
  ssize_t wrote = write(wake_fds[1], &one, 1);
  (void)wrote;
}
