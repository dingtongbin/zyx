/* The wakeup handle. A thread outside the scheduler signals it, and the
   scheduler waits on it, so work that arrives from another thread runs rather
   than waiting for the next deadline.

   The handle is one word and the wait the operating system keeps for that
   address. A waiter compares the word it saw with the one in memory and sleeps
   only if they still agree, so a signal costs no system call at all when the
   scheduler is already awake, and a signal that arrives between the compare
   and the sleep turns into an immediate return. Every signal moves the word,
   which is also what makes releasing every waiter exact. */

#ifndef ZYXRUNTIME_WAKEUP_H
#define ZYXRUNTIME_WAKEUP_H

#include "zyxrt_internal.h"

#if defined(_WIN32)

#include <windows.h>

/* The wait and the two wakes are taken from the library at startup: they reach
   a forwarded import the platform's import libraries do not carry, so neither
   the linker nor one fixed library name finds them. The first library that has
   them answers. */
typedef BOOL(WINAPI *zyxrt_note_wait_fn)(volatile VOID *, PVOID, SIZE_T, DWORD);
typedef VOID(WINAPI *zyxrt_note_wake_fn)(PVOID);

static zyxrt_note_wait_fn zyxrt_note_wait;
static zyxrt_note_wake_fn zyxrt_note_wake_one;
static zyxrt_note_wake_fn zyxrt_note_wake_all;
static _Atomic LONG zyxrt_wakeup_note;

void zyxrt_wakeup_init(void) {
  static const char *libraries[] = {
      "api-ms-win-core-synch-l1-2-0.dll",
      "kernelbase.dll",
      "kernel32.dll",
  };
  for (size_t i = 0; i < sizeof(libraries) / sizeof(libraries[0]); i++) {
    HMODULE lib = GetModuleHandleA(libraries[i]);
    if (lib == NULL) continue;
    zyxrt_note_wait = (zyxrt_note_wait_fn)(void *)GetProcAddress(lib, "WaitOnAddress");
    zyxrt_note_wake_one = (zyxrt_note_wake_fn)(void *)GetProcAddress(lib, "WakeByAddressSingle");
    zyxrt_note_wake_all = (zyxrt_note_wake_fn)(void *)GetProcAddress(lib, "WakeByAddressAll");
    if (zyxrt_note_wait != NULL && zyxrt_note_wake_one != NULL && zyxrt_note_wake_all != NULL) break;
  }
  if (zyxrt_note_wait == NULL || zyxrt_note_wake_one == NULL || zyxrt_note_wake_all == NULL) {
    zyxrt_panic("the address wait is not present", 31);
  }
  atomic_store_explicit(&zyxrt_wakeup_note, 0, memory_order_relaxed);
}

void zyxrt_wakeup_signal(void) {
  atomic_fetch_add_explicit(&zyxrt_wakeup_note, 1, memory_order_release);
  zyxrt_note_wake_one((PVOID)&zyxrt_wakeup_note);
}

void zyxrt_wakeup_signal_all(size_t count) {
  if (count == 0) return;
  atomic_fetch_add_explicit(&zyxrt_wakeup_note, (LONG)count, memory_order_release);
  zyxrt_note_wake_all((PVOID)&zyxrt_wakeup_note);
}

void zyxrt_wakeup_wait(int64_t ns) {
  LONG seen = atomic_load_explicit(&zyxrt_wakeup_note, memory_order_acquire);
  DWORD ms = INFINITE;
  if (ns >= 0) {
    ms = (DWORD)((ns + 999999) / 1000000);
    if (ms == 0) ms = 1;
  }
  zyxrt_note_wait((volatile VOID *)&zyxrt_wakeup_note, &seen, sizeof(seen), ms);
}

#elif defined(__linux__) || defined(__ANDROID__)

#include <linux/futex.h>
#include <sys/syscall.h>
#include <unistd.h>

static _Atomic int zyxrt_wakeup_note;

static int note_wait(int seen, const struct timespec *timeout) {
  return (int)syscall(SYS_futex, (int *)&zyxrt_wakeup_note, FUTEX_WAIT, seen, timeout, NULL, 0);
}

void zyxrt_wakeup_init(void) { atomic_store_explicit(&zyxrt_wakeup_note, 0, memory_order_relaxed); }

void zyxrt_wakeup_signal(void) {
  atomic_fetch_add_explicit(&zyxrt_wakeup_note, 1, memory_order_release);
  syscall(SYS_futex, (int *)&zyxrt_wakeup_note, FUTEX_WAKE, 1, NULL, NULL, 0);
}

void zyxrt_wakeup_signal_all(size_t count) {
  if (count == 0) return;
  atomic_fetch_add_explicit(&zyxrt_wakeup_note, (int)count, memory_order_release);
  syscall(SYS_futex, (int *)&zyxrt_wakeup_note, FUTEX_WAKE, (int)count, NULL, NULL, 0);
}

void zyxrt_wakeup_wait(int64_t ns) {
  int seen = atomic_load_explicit(&zyxrt_wakeup_note, memory_order_acquire);
  struct timespec ts;
  const struct timespec *timeout = NULL;
  if (ns >= 0) {
    ts.tv_sec = (time_t)(ns / 1000000000u);
    ts.tv_nsec = (long)(ns % 1000000000u);
    timeout = &ts;
  }
  note_wait(seen, timeout);
}

#else

/* A target with neither an address wait nor a futex waits on a pipe, which
   costs a system call per signal but behaves the same way. */
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <unistd.h>

static int zyxrt_wakeup_fds[2] = {-1, -1};

void zyxrt_wakeup_init(void) {
  if (pipe(zyxrt_wakeup_fds) != 0) zyxrt_panic("cannot create the wakeup pipe", 30);
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
