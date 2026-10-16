/* Event loop backend: epoll. The kernel set is the state, so registering or
   dropping a descriptor from the scheduler thread reaches a poller that is
   already waiting, and a pipe gives the scheduler a way to make the poller
   look again. */

#include "zyxrt_internal.h"

#include <errno.h>
#include <sys/epoll.h>
#include <unistd.h>

static int epoll_fd = -1;
static int wake_fds[2] = {-1, -1};

int zyxrt_loop_backend_init(void) {
  epoll_fd = epoll_create1(0);
  if (epoll_fd < 0) return -1;
  if (pipe(wake_fds) != 0) {
    close(epoll_fd);
    epoll_fd = -1;
    return -1;
  }
  struct epoll_event ev;
  ev.events = EPOLLIN;
  ev.data.fd = -1;
  if (epoll_ctl(epoll_fd, EPOLL_CTL_ADD, wake_fds[0], &ev) != 0) {
    close(wake_fds[0]);
    close(wake_fds[1]);
    close(epoll_fd);
    wake_fds[0] = wake_fds[1] = -1;
    epoll_fd = -1;
    return -1;
  }
  return 0;
}

int zyxrt_loop_backend_add(int fd, int events) {
  if (epoll_fd < 0) return -1;
  struct epoll_event ev;
  ev.events = 0;
  if (events & ZYXRT_FD_READ) ev.events |= EPOLLIN;
  if (events & ZYXRT_FD_WRITE) ev.events |= EPOLLOUT;
  ev.data.fd = fd;
  if (events == 0) {
    /* Dropping a descriptor the kernel does not watch is not a failure. */
    if (epoll_ctl(epoll_fd, EPOLL_CTL_DEL, fd, &ev) != 0 && errno != ENOENT && errno != EBADF) {
      return -1;
    }
    return 0;
  }
  if (epoll_ctl(epoll_fd, EPOLL_CTL_ADD, fd, &ev) == 0) return 0;
  if (errno != EEXIST) return -1;
  return epoll_ctl(epoll_fd, EPOLL_CTL_MOD, fd, &ev) == 0 ? 0 : -1;
}

int zyxrt_loop_backend_wait(int *fds, int *events, int max, int timeout_ms) {
  if (epoll_fd < 0) return -1;
  struct epoll_event ready[ZYXRT_LOOP_BATCH];
  int count = max < ZYXRT_LOOP_BATCH ? max : ZYXRT_LOOP_BATCH;
  int n = epoll_wait(epoll_fd, ready, count, timeout_ms);
  if (n < 0) {
    return errno == EINTR ? 0 : -1;
  }
  int out = 0;
  for (int i = 0; i < n; i++) {
    if (ready[i].data.fd < 0) {
      /* The wakeup pipe: one read takes whatever the signals left. */
      char buf[64];
      ssize_t got = read(wake_fds[0], buf, sizeof(buf));
      (void)got;
      continue;
    }
    int ev = 0;
    if (ready[i].events & (EPOLLIN | EPOLLHUP | EPOLLERR)) ev |= ZYXRT_FD_READ;
    if (ready[i].events & EPOLLOUT) ev |= ZYXRT_FD_WRITE;
    if (ev == 0) continue;
    fds[out] = ready[i].data.fd;
    events[out] = ev;
    out++;
  }
  return out;
}

void zyxrt_loop_backend_wake(void) {
  if (wake_fds[1] < 0) return;
  char one = 1;
  ssize_t wrote = write(wake_fds[1], &one, 1);
  (void)wrote;
}
