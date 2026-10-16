/* Cooperative preemption. A coroutine that computes without blocking holds the
   scheduler thread, and every other coroutine on that thread waits. The
   compiler puts a check on every loop back edge, and this file decides when a
   check lets another coroutine run: a monitor thread watches how long the
   running coroutine has held the thread and marks it once it has had long
   enough. The check itself is one atomic load, so a loop that never exceeds
   the quantum pays nothing for the mechanism beyond the load. */

#include "zyxrt_internal.h"
#include "thread.h"

#include <stdatomic.h>

/* The coroutine the scheduler is running. The scheduler writes it on every
   switch, so it is atomic: the monitor reads it from another thread. */
static _Atomic(zyxrt_coro *) running;

/* running_since is the monotonic time the running coroutine was resumed. */
static _Atomic uint64_t running_since;

/* The quantum is how long a coroutine may hold the scheduler thread before the
   monitor asks it to let another one run. A tenth of a second is long enough
   that the check is rare and short enough that a coroutine that never blocks
   cannot delay the rest of the program by more than that. */
#define ZYXXRT_QUANTUM_NS (10u * 1000u * 1000u)

/* The monitor sleeps for half the quantum, so a coroutine that has held the
   thread for a whole one is marked on the next pass. */
#define ZYXXRT_MONITOR_NS (ZYXXRT_QUANTUM_NS / 2u)

/* monitor marks the running coroutine once it has held the scheduler thread
   for a whole quantum. It runs only where the target has threads. */
static void monitor(void *arg) {
  (void)arg;
  for (;;) {
    zyxrt_sleep(ZYXXRT_MONITOR_NS);
    zyxrt_coro *c = atomic_load_explicit(&running, memory_order_acquire);
    if (c == NULL) {
      continue;
    }
    uint64_t since = atomic_load_explicit(&running_since, memory_order_relaxed);
    if (zyxrt_now() - since < ZYXXRT_QUANTUM_NS) {
      continue;
    }
    atomic_store_explicit(&c->preempt, 1, memory_order_release);
  }
}

void zyxrt_preempt_init(void) {
  if (!zyxrt_threads()) {
    return;
  }
  atomic_store_explicit(&running, NULL, memory_order_relaxed);
  atomic_store_explicit(&running_since, 0, memory_order_relaxed);
  zyxrt_thread_start(monitor, NULL);
}

/* zyxrt_preempt_enter records the coroutine the scheduler is about to run,
   which is the one the monitor watches. */
void zyxrt_preempt_enter(zyxrt_coro *c) {
  atomic_store_explicit(&running_since, zyxrt_now(), memory_order_relaxed);
  atomic_store_explicit(&running, c, memory_order_release);
}

/* zyxrt_preempt_leave records that no coroutine is running, which is the state
   the scheduler is in while it chooses the next one. */
void zyxrt_preempt_leave(void) {
  atomic_store_explicit(&running, NULL, memory_order_release);
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
