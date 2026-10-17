/* The wakeup handle, of which the runtime keeps one per place that waits: the
   scheduler's threads wait on one, and the preemption monitor on another. A
   signal from another thread moves that word and wakes its waiters, so work
   that arrives from outside the scheduler runs rather than waiting for the next
   deadline.
 *
 * A note is one word and the wait the operating system keeps for that address.
 * A waiter compares the word it saw with the one in memory and sleeps only if
 * they still agree, so a signal costs no system call at all when the waiter is
 * already awake, and a signal that arrives between the compare and the sleep
 * turns into an immediate return. Every signal moves the word, which is also
 * what makes releasing every waiter exact.
 *
 * The word, and the way to wait on it, differ by platform; the operations are
 * defined once, in note.c. */

#ifndef ZYXRUNTIME_WAKEUP_H
#define ZYXRUNTIME_WAKEUP_H

#include <stddef.h>
#include <stdint.h>
#include <stdatomic.h>

#if defined(__wasm__)

/* A target without threads has nobody to wake, so a note is a word nothing
   reads and nothing moves. */
typedef struct zyxrt_note {
  int unused;
} zyxrt_note;

#elif defined(_WIN32)

#include <windows.h>

/* The word the wait compares against. */
typedef struct zyxrt_note {
  _Atomic LONG word;
} zyxrt_note;

#elif defined(__linux__) || defined(__ANDROID__)

/* The word the futex waits on. */
typedef struct zyxrt_note {
  _Atomic int word;
} zyxrt_note;

#else

/* A target whose only wait is a pipe: a byte per wake, which the waiters read
   as they come. */
typedef struct zyxrt_note {
  int fd_read;
  int fd_write;
} zyxrt_note;

#endif

/* zyxrt_note_init prepares a note, which is what a waiter and a signaller both
   need before either touches it. */
void zyxrt_note_init(zyxrt_note *n);
/* zyxrt_note_signal releases one waiter and zyxrt_note_signal_all releases
   count of them. */
void zyxrt_note_signal(zyxrt_note *n);
void zyxrt_note_signal_all(zyxrt_note *n, size_t count);
/* zyxrt_note_wait waits for the note, up to ns nanoseconds, or forever when ns
   is negative. */
void zyxrt_note_wait(zyxrt_note *n, int64_t ns);

#endif /* ZYXRUNTIME_WAKEUP_H */
