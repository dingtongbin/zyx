/* The wakeup handle. A thread outside the scheduler signals it, and the
   scheduler waits on it, so work that arrives from another thread runs rather
   than waiting for the next deadline. The handle counts the signals rather
   than holding one flag, because a thread that finds the program over releases
   every thread waiting on it at once; a handle that holds one wake would leave
   the rest waiting for a wake that never comes. */

#ifndef ZYXRUNTIME_WAKEUP_H
#define ZYXRUNTIME_WAKEUP_H

#include "zyxrt_internal.h"

#if defined(_WIN32)

#include <windows.h>

static HANDLE zyxrt_wakeup_handle;

void zyxrt_wakeup_init(void) {
  zyxrt_wakeup_handle = CreateSemaphoreA(NULL, 0, 0x7fffffffl, NULL);
  if (!zyxrt_wakeup_handle) zyxrt_panic("cannot create the wakeup handle", 32);
}

void zyxrt_wakeup_signal(void) {
  if (zyxrt_wakeup_handle) ReleaseSemaphore(zyxrt_wakeup_handle, 1, NULL);
}

void zyxrt_wakeup_signal_all(size_t count) {
  if (!zyxrt_wakeup_handle || count == 0) return;
  if (count > 0x7fffffffu) count = 0x7fffffffu;
  ReleaseSemaphore(zyxrt_wakeup_handle, (LONG)count, NULL);
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

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <unistd.h>

static int zyxrt_wakeup_fds[2] = {-1, -1};

void zyxrt_wakeup_init(void) {
  if (pipe(zyxrt_wakeup_fds) != 0) zyxrt_panic("cannot create the wakeup pipe", 30);
  /* A full pipe means a signal is already pending for every byte it holds, so
     a signal that cannot be written is one no waiter needs. */
  fcntl(zyxrt_wakeup_fds[1], F_SETFL, O_NONBLOCK);
}

void zyxrt_wakeup_signal(void) {
  if (zyxrt_wakeup_fds[1] < 0) return;
  char one = 1;
  ssize_t written = write(zyxrt_wakeup_fds[1], &one, 1);
  (void)written;
}

void zyxrt_wakeup_signal_all(size_t count) {
  char signals[64];
  while (count > 0) {
    size_t batch = count < sizeof(signals) ? count : sizeof(signals);
    ssize_t written = write(zyxrt_wakeup_fds[1], signals, batch);
    if (written <= 0) return;
    count -= (size_t)written;
  }
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
    /* One byte is one wake. A wait that took more than one would swallow the
       wakes of the threads that never got to read them. */
    char one;
    ssize_t got = read(zyxrt_wakeup_fds[0], &one, 1);
    (void)got;
  }
}

#endif

#endif /* ZYXRUNTIME_WAKEUP_H */
