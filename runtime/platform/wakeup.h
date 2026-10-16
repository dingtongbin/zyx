/* The wakeup handle. A thread outside the scheduler signals it, and the
   scheduler waits on it, so work that arrives from another thread runs rather
   than waiting for the next deadline. */

#ifndef ZYXRUNTIME_WAKEUP_H
#define ZYXRUNTIME_WAKEUP_H

#include "zyxrt_internal.h"

#if defined(_WIN32)

#include <windows.h>

static HANDLE zyxrt_wakeup_handle;

void zyxrt_wakeup_init(void) {
  zyxrt_wakeup_handle = CreateEvent(NULL, FALSE, FALSE, NULL);
  if (!zyxrt_wakeup_handle) zyxrt_panic("cannot create the wakeup handle", 32);
}

void zyxrt_wakeup_signal(void) {
  if (zyxrt_wakeup_handle) SetEvent(zyxrt_wakeup_handle);
}

void zyxrt_wakeup_wait(int64_t ns) {
  if (!zyxrt_wakeup_handle) return;
  DWORD ms = INFINITE;
  if (ns >= 0) {
    ms = (DWORD)((ns + 999999) / 1000000);
    if (ms == 0) ms = 1;
  }
  WaitForSingleObject(zyxrt_wakeup_handle, ms);
}

#else

#include <poll.h>
#include <unistd.h>

static int zyxrt_wakeup_fds[2] = {-1, -1};

void zyxrt_wakeup_init(void) {
  if (pipe(zyxrt_wakeup_fds) != 0) zyxrt_panic("cannot create the wakeup pipe", 30);
}

void zyxrt_wakeup_signal(void) {
  if (zyxrt_wakeup_fds[1] < 0) return;
  char one = 1;
  ssize_t written = write(zyxrt_wakeup_fds[1], &one, 1);
  (void)written;
}

void zyxrt_wakeup_wait(int64_t ns) {
  if (zyxrt_wakeup_fds[0] < 0) return;
  struct pollfd pfd;
  pfd.fd = zyxrt_wakeup_fds[0];
  pfd.events = POLLIN;
  pfd.revents = 0;
  int timeout = -1;
  if (ns >= 0) {
    timeout = (int)((ns + 999999) / 1000000);
    if (timeout == 0) timeout = 1;
  }
  if (poll(&pfd, 1, timeout) > 0) {
    /* One read takes whatever the signals left, which is what empties it. */
    char buf[64];
    ssize_t read_bytes = read(zyxrt_wakeup_fds[0], buf, sizeof(buf));
    (void)read_bytes;
  }
}

#endif

#endif /* ZYXRUNTIME_WAKEUP_H */
