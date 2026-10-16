/* Event loop backend: kqueue. The kernel set is the state, so registering or
   dropping a descriptor from the scheduler thread reaches a poller that is
   already waiting, and a user event gives the scheduler a way to make the
   poller look again. */

#include "zyxrt_internal.h"

#include <errno.h>
#include <sys/event.h>
#include <sys/time.h>
#include <unistd.h>

static int kq_fd = -1;

int zyxrt_loop_backend_init(void) {
  kq_fd = kqueue();
  if (kq_fd < 0) return -1;
  struct kevent ev;
  EV_SET(&ev, 0, EVFILT_USER, EV_ADD | EV_CLEAR, 0, 0, NULL);
  if (kevent(kq_fd, &ev, 1, NULL, 0, NULL) != 0) {
    close(kq_fd);
    kq_fd = -1;
    return -1;
  }
  return 0;
}

int zyxrt_loop_backend_add(int fd, int events) {
  if (kq_fd < 0) return -1;
  struct kevent evs[2];
  EV_SET(&evs[0], fd, EVFILT_READ,
         (events & ZYXRT_FD_READ) ? (EV_ADD | EV_ENABLE) : EV_DELETE, 0, 0, NULL);
  EV_SET(&evs[1], fd, EVFILT_WRITE,
         (events & ZYXRT_FD_WRITE) ? (EV_ADD | EV_ENABLE) : EV_DELETE, 0, 0, NULL);
  /* Dropping a filter the kernel does not hold is not a failure. */
  if (kevent(kq_fd, evs, 2, NULL, 0, NULL) != 0 && errno != ENOENT && errno != EBADF) {
    return -1;
  }
  return 0;
}

int zyxrt_loop_backend_wait(int *fds, int *events, int max, int timeout_ms) {
  if (kq_fd < 0) return -1;
  struct kevent ready[ZYXRT_LOOP_BATCH];
  int count = max < ZYXRT_LOOP_BATCH ? max : ZYXRT_LOOP_BATCH;
  struct timespec ts;
  struct timespec *limit = NULL;
  if (timeout_ms >= 0) {
    ts.tv_sec = timeout_ms / 1000;
    ts.tv_nsec = (long)(timeout_ms % 1000) * 1000000L;
    limit = &ts;
  }
  int n = kevent(kq_fd, NULL, 0, ready, count, limit);
  if (n < 0) {
    return errno == EINTR ? 0 : -1;
  }
  int out = 0;
  for (int i = 0; i < n; i++) {
    if (ready[i].filter == EVFILT_USER) continue;
    int ev = 0;
    if (ready[i].filter == EVFILT_READ) ev |= ZYXRT_FD_READ;
    if (ready[i].filter == EVFILT_WRITE) ev |= ZYXRT_FD_WRITE;
    if (ev == 0) continue;
    fds[out] = (int)ready[i].ident;
    events[out] = ev;
    out++;
  }
  return out;
}

void zyxrt_loop_backend_wake(void) {
  if (kq_fd < 0) return;
  struct kevent ev;
  EV_SET(&ev, 0, EVFILT_USER, 0, NOTE_TRIGGER, 0, NULL);
  kevent(kq_fd, &ev, 1, NULL, 0, NULL);
}
