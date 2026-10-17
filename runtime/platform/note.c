/* The wakeup handle, one implementation for every target. The wait and the
   wakes come from the operating system where it has an address wait, from the
   futex system call where it has that, and from a pipe elsewhere; the word each
   of them waits on, and what else the handle holds, is in wakeup.h. */

#include "zyxrt_internal.h"
#include "wakeup.h"

#if defined(__wasm__)

void zyxrt_note_init(zyxrt_note *n) { (void)n; }
void zyxrt_note_signal(zyxrt_note *n) { (void)n; }
void zyxrt_note_signal_all(zyxrt_note *n, size_t count) {
  (void)n;
  (void)count;
}
void zyxrt_note_wait(zyxrt_note *n, int64_t ns) {
  (void)n;
  (void)ns;
}

#elif defined(_WIN32)

/* The wait and the two wakes are taken from the library at startup: they reach
   a forwarded import the platform's import libraries do not carry, so neither
   the linker nor one fixed library name finds them. The first library that has
   them answers. */
typedef BOOL(WINAPI *note_wait_fn)(volatile VOID *, PVOID, SIZE_T, DWORD);
typedef VOID(WINAPI *note_wake_fn)(PVOID);

static note_wait_fn note_wait_entry;
static note_wake_fn note_wake_one_entry;
static note_wake_fn note_wake_all_entry;

void zyxrt_note_init(zyxrt_note *n) {
  static const char *libraries[] = {
      "api-ms-win-core-synch-l1-2-0.dll",
      "kernelbase.dll",
      "kernel32.dll",
  };
  if (note_wait_entry == NULL) {
    for (size_t i = 0; i < sizeof(libraries) / sizeof(libraries[0]); i++) {
      HMODULE lib = GetModuleHandleA(libraries[i]);
      if (lib == NULL) continue;
      note_wait_entry = (note_wait_fn)(void *)GetProcAddress(lib, "WaitOnAddress");
      note_wake_one_entry = (note_wake_fn)(void *)GetProcAddress(lib, "WakeByAddressSingle");
      note_wake_all_entry = (note_wake_fn)(void *)GetProcAddress(lib, "WakeByAddressAll");
      if (note_wait_entry != NULL && note_wake_one_entry != NULL && note_wake_all_entry != NULL) break;
    }
    if (note_wait_entry == NULL || note_wake_one_entry == NULL || note_wake_all_entry == NULL) {
      zyxrt_panic("the address wait is not present", 31);
    }
  }
  atomic_store_explicit(&n->word, 0, memory_order_relaxed);
}

void zyxrt_note_signal(zyxrt_note *n) {
  atomic_fetch_add_explicit(&n->word, 1, memory_order_release);
  note_wake_one_entry((PVOID)&n->word);
}

void zyxrt_note_signal_all(zyxrt_note *n, size_t count) {
  if (count == 0) return;
  atomic_fetch_add_explicit(&n->word, (LONG)count, memory_order_release);
  note_wake_all_entry((PVOID)&n->word);
}

void zyxrt_note_wait(zyxrt_note *n, int64_t ns) {
  LONG seen = atomic_load_explicit(&n->word, memory_order_acquire);
  DWORD ms = INFINITE;
  if (ns >= 0) {
    ms = (DWORD)((ns + 999999) / 1000000);
    if (ms == 0) ms = 1;
  }
  note_wait_entry((volatile VOID *)&n->word, &seen, sizeof(seen), ms);
}

#elif defined(__linux__) || defined(__ANDROID__)

#include <linux/futex.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

static int note_futex(int *word, int operation, int value, const struct timespec *timeout) {
  return (int)syscall(SYS_futex, word, operation, value, timeout, NULL, 0);
}

void zyxrt_note_init(zyxrt_note *n) { atomic_store_explicit(&n->word, 0, memory_order_relaxed); }

void zyxrt_note_signal(zyxrt_note *n) {
  atomic_fetch_add_explicit(&n->word, 1, memory_order_release);
  note_futex((int *)&n->word, FUTEX_WAKE, 1, NULL);
}

void zyxrt_note_signal_all(zyxrt_note *n, size_t count) {
  if (count == 0) return;
  atomic_fetch_add_explicit(&n->word, (int)count, memory_order_release);
  note_futex((int *)&n->word, FUTEX_WAKE, (int)count, NULL);
}

void zyxrt_note_wait(zyxrt_note *n, int64_t ns) {
  int seen = atomic_load_explicit(&n->word, memory_order_acquire);
  struct timespec ts;
  const struct timespec *timeout = NULL;
  if (ns >= 0) {
    ts.tv_sec = (time_t)(ns / 1000000000u);
    ts.tv_nsec = (long)(ns % 1000000000u);
    timeout = &ts;
  }
  note_futex((int *)&n->word, FUTEX_WAIT, seen, timeout);
}

#else

/* A target with neither an address wait nor a futex waits on a pipe, which
   costs a system call per signal but behaves the same way. */
#include <fcntl.h>
#include <poll.h>
#include <unistd.h>

void zyxrt_note_init(zyxrt_note *n) {
  int fds[2];
  if (pipe(fds) != 0) zyxrt_panic("cannot create the wakeup pipe", 30);
  fcntl(fds[1], F_SETFL, O_NONBLOCK);
  n->fd_read = fds[0];
  n->fd_write = fds[1];
}

void zyxrt_note_signal(zyxrt_note *n) {
  char one = 1;
  ssize_t written = write(n->fd_write, &one, 1);
  (void)written;
}

void zyxrt_note_signal_all(zyxrt_note *n, size_t count) {
  char signals[64];
  while (count > 0) {
    size_t batch = count < sizeof(signals) ? count : sizeof(signals);
    ssize_t written = write(n->fd_write, signals, batch);
    if (written <= 0) return;
    count -= (size_t)written;
  }
}

void zyxrt_note_wait(zyxrt_note *n, int64_t ns) {
  struct pollfd pfd;
  pfd.fd = n->fd_read;
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
    ssize_t got = read(n->fd_read, &one, 1);
    (void)got;
  }
}

#endif
