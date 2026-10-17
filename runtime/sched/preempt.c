/* Cooperative preemption. A coroutine that computes without blocking holds its
   scheduler thread, and every other coroutine on that thread waits. The
   compiler puts a check on every loop back edge, and this file decides when a
   check lets another coroutine run: a monitor thread watches how long each
   scheduler thread has run its coroutine and marks it once it has had long
   enough. The check itself is one atomic load, so a loop that never exceeds
   the quantum pays nothing for the mechanism beyond the load. */

#include "zyxrt_internal.h"
#include "wakeup.h"

#include <stdatomic.h>

/* One slot per scheduler thread, named by the thread's index. The monitor
   walks them, so a busy thread is marked whoever else is running. A thread
   that is not a scheduler thread has no slot and is never watched. */

static _Atomic(zyxrt_coro *) running[ZYXXRT_PREEMPT_SLOTS];

/* running_since is the monotonic time the thread resumed its coroutine. */
static _Atomic uint64_t running_since[ZYXXRT_PREEMPT_SLOTS];

/* self_slot answers the calling thread's slot, or
   ZYXXRT_PREEMPT_NONE when the thread is not a scheduler thread. The answer
   comes from the scheduler rather than from a thread-local variable, because a
   coroutine's saved context carries the storage of the thread it was saved
   on, and a coroutine may be resumed elsewhere. */
#define ZYXXRT_PREEMPT_NONE ((size_t)-1)
static size_t self_slot(void) {
  size_t index = zyxrt_sched_thread_index();
  return index < ZYXXRT_PREEMPT_SLOTS ? index : ZYXXRT_PREEMPT_NONE;
}

/* The quantum is how long a coroutine may hold a scheduler thread before the
   monitor asks it to let another one run. Ten milliseconds is long enough
   that the check is rare and short enough that a coroutine that never blocks
   cannot delay the rest of the program by more than that. */
#define ZYXXRT_QUANTUM_NS (10u * 1000u * 1000u)

/* The monitor sleeps for half the quantum, so a coroutine that has held a
   thread for a whole one is marked on the next pass. */
#define ZYXXRT_MONITOR_NS (ZYXXRT_QUANTUM_NS / 2u)

/* monitor_quitting tells the monitor to leave. The monitor sleeps on a note of
   its own, so telling it to leave wakes it rather than waiting for a sleep to
   end, and the thread that started it waits for it to return. */
static zyxrt_note monitor_note;
static _Atomic int monitor_quitting;
static zyxrt_thread_handle monitor_handle;
static int monitor_started;

/* monitor marks every coroutine that has held its scheduler thread for a whole
   quantum. It runs only where the target has threads. */
static void monitor(void *arg) {
  (void)arg;
  while (!atomic_load_explicit(&monitor_quitting, memory_order_acquire)) {
    zyxrt_note_wait(&monitor_note, ZYXXRT_MONITOR_NS);
    for (size_t i = 0; i < ZYXXRT_PREEMPT_SLOTS; i++) {
      zyxrt_coro *c = atomic_load_explicit(&running[i], memory_order_acquire);
      if (c == NULL) {
        continue;
      }
      uint64_t since = atomic_load_explicit(&running_since[i], memory_order_relaxed);
      if (zyxrt_now() - since < ZYXXRT_QUANTUM_NS) {
        continue;
      }
      atomic_store_explicit(&c->preempt, 1, memory_order_release);
    }
  }
}

void zyxrt_preempt_stop(void) {
  if (!monitor_started) return;
  atomic_store_explicit(&monitor_quitting, 1, memory_order_release);
  zyxrt_note_signal(&monitor_note);
  zyxrt_thread_join(monitor_handle);
}

/* zyxrt_preempt_start starts the monitor. The scheduler asks for it once, so a
   program that runs coroutines has one and a program that does not pays for
   none. */
void zyxrt_preempt_start(void) {
  if (!zyxrt_threads() || monitor_started) {
    return;
  }
  monitor_started = 1;
  zyxrt_note_init(&monitor_note);
  monitor_handle = zyxrt_thread_start(monitor, NULL);
}

/* zyxrt_preempt_enter records the coroutine the calling thread is about to
   run, which is the one the monitor watches. */
void zyxrt_preempt_enter(zyxrt_coro *c) {
  size_t slot = self_slot();
  if (slot == ZYXXRT_PREEMPT_NONE) {
    return;
  }
  atomic_store_explicit(&running_since[slot], zyxrt_now(), memory_order_relaxed);
  atomic_store_explicit(&running[slot], c, memory_order_release);
}

/* zyxrt_preempt_leave records that the calling thread runs no coroutine, which
   is the state it is in while it chooses the next one. */
void zyxrt_preempt_leave(void) {
  size_t slot = self_slot();
  if (slot == ZYXXRT_PREEMPT_NONE) {
    return;
  }
  atomic_store_explicit(&running[slot], NULL, memory_order_release);
}

/* zyxrt_preempt_check is what a loop back edge calls. A marked coroutine gives
   the scheduler thread up and is resumed when it is ready again, which is what
   keeps one coroutine's computation from delaying every other one. */
void zyxrt_preempt_check(void) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (self == NULL) {
    return;
  }
  if (atomic_exchange_explicit(&self->preempt, 0, memory_order_acq_rel)) {
    zyxrt_sched_yield();
  }
}
