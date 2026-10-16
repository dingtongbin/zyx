/* Cooperative preemption. A coroutine that computes without blocking holds its
   scheduler thread, and every other coroutine on that thread waits. The
   compiler puts a check on every loop back edge, and this file decides when a
   check lets another coroutine run: a monitor thread watches how long each
   scheduler thread has run its coroutine and marks it once it has had long
   enough. The check itself is one atomic load, so a loop that never exceeds
   the quantum pays nothing for the mechanism beyond the load. */

#include "zyxrt_internal.h"

#include <stdatomic.h>

/* One slot per scheduler thread, claimed when the thread starts. The monitor
   walks them, so a busy thread is marked whoever else is running. */
#define ZYXXRT_PREEMPT_SLOTS 256

static _Atomic(zyxrt_coro *) running[ZYXXRT_PREEMPT_SLOTS];

/* running_since is the monotonic time the thread resumed its coroutine. */
static _Atomic uint64_t running_since[ZYXXRT_PREEMPT_SLOTS];

static _Atomic size_t slot_count;

/* self_slot is the calling thread's slot, or ZYXXRT_PREEMPT_NONE when the
   thread is not a scheduler thread. */
#define ZYXXRT_PREEMPT_NONE ((size_t)-1)
static _Thread_local size_t self_slot = ZYXXRT_PREEMPT_NONE;

/* The quantum is how long a coroutine may hold a scheduler thread before the
   monitor asks it to let another one run. Ten milliseconds is long enough
   that the check is rare and short enough that a coroutine that never blocks
   cannot delay the rest of the program by more than that. */
#define ZYXXRT_QUANTUM_NS (10u * 1000u * 1000u)

/* The monitor sleeps for half the quantum, so a coroutine that has held a
   thread for a whole one is marked on the next pass. */
#define ZYXXRT_MONITOR_NS (ZYXXRT_QUANTUM_NS / 2u)

/* monitor marks every coroutine that has held its scheduler thread for a whole
   quantum. It runs only where the target has threads. */
static void monitor(void *arg) {
  (void)arg;
  for (;;) {
    zyxrt_sleep(ZYXXRT_MONITOR_NS);
    size_t slots = atomic_load_explicit(&slot_count, memory_order_acquire);
    for (size_t i = 0; i < slots; i++) {
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

void zyxrt_preempt_init(void) {
  if (!zyxrt_threads()) {
    return;
  }
  atomic_store_explicit(&slot_count, 0, memory_order_relaxed);
  for (size_t i = 0; i < ZYXXRT_PREEMPT_SLOTS; i++) {
    atomic_store_explicit(&running[i], NULL, memory_order_relaxed);
  }
  zyxrt_thread_start(monitor, NULL);
}

/* zyxrt_preempt_thread gives the calling scheduler thread a slot of its own. */
void zyxrt_preempt_thread(void) {
  if (!zyxrt_threads()) {
    return;
  }
  size_t slot = atomic_fetch_add_explicit(&slot_count, 1, memory_order_acq_rel);
  if (slot < ZYXXRT_PREEMPT_SLOTS) {
    self_slot = slot;
  }
}

/* zyxrt_preempt_enter records the coroutine the calling thread is about to
   run, which is the one the monitor watches. */
void zyxrt_preempt_enter(zyxrt_coro *c) {
  if (self_slot == ZYXXRT_PREEMPT_NONE) {
    return;
  }
  atomic_store_explicit(&running_since[self_slot], zyxrt_now(), memory_order_relaxed);
  atomic_store_explicit(&running[self_slot], c, memory_order_release);
}

/* zyxrt_preempt_leave records that the calling thread runs no coroutine, which
   is the state it is in while it chooses the next one. */
void zyxrt_preempt_leave(void) {
  if (self_slot == ZYXXRT_PREEMPT_NONE) {
    return;
  }
  atomic_store_explicit(&running[self_slot], NULL, memory_order_release);
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
