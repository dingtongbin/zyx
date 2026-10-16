/* Cooperative coroutine scheduler. Coroutines run on their own stacks; a
   coroutine that cannot proceed suspends itself and the scheduler thread
   resumes the next ready one.
 *
 * The scheduler runs on as many threads as ZYX_THREADS names, one per core by
 * default, and each thread has a queue of its own. A coroutine may run on any
 * of them, and may move from one to another, so everything the scheduler
 * shares is either owned by one thread (a queue) or guarded (the count of
 * idle threads, the decision that the program is over). */

#include "zyxrt_internal.h"

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

/* A scheduler thread. Every thread owns a queue of coroutines to run and the
   context a suspended coroutine switches back to. */
typedef struct zyxrt_thread {
  zyxrt_coro *head;
  zyxrt_coro *tail;
  zyxrt_lock lock;
  /* ctx is the thread's own context, which a suspended coroutine switches
     back to. */
  void *ctx;
  /* current is the coroutine the thread is running, or NULL. */
  zyxrt_coro *current;
  size_t index;
} zyxrt_thread;

/* self is the thread state of the scheduler thread the caller runs on, or
   NULL for a thread that is not part of the scheduler. */
static _Thread_local zyxrt_thread *self_thread;

static zyxrt_thread *threads;
static size_t thread_count;

/* Work from outside the scheduler lands here: a blocking pool thread that
   finished a job, a poller thread that found a descriptor ready, the
   preemption monitor, or a scheduler thread that is shutting down. The head
   is atomic so a thread can tell that something arrived without taking the
   lock, which is what keeps a thread with a queue of its own from never
   looking. */
static _Atomic(zyxrt_coro *) incoming_head;
static zyxrt_coro *incoming_tail;
static zyxrt_lock incoming_lock;

/* state_lock guards the count of idle threads and the decision that the
   program is over. */
static zyxrt_lock state_lock;
static size_t idle_threads;
static _Atomic int stopping;

/* externals counts the coroutines waiting for something the scheduler cannot
   see, which is what keeps a thread from deciding the program is over while
   work is still on its way. */
static _Atomic int externals;

void zyxrt_sched_external(int delta) {
  atomic_fetch_add_explicit(&externals, delta, memory_order_acq_rel);
}

/* queue_push links a coroutine onto a queue. The lock covers the links, and
   the scheduling word says which queue may hold it at all. */
static void queue_push(zyxrt_coro **head, zyxrt_coro **tail, zyxrt_lock *lock, zyxrt_coro *c) {
  c->wait_next = NULL;
  zyxrt_lock_hold(lock);
  if (*tail != NULL) {
    (*tail)->wait_next = c;
  } else {
    *head = c;
  }
  *tail = c;
  zyxrt_lock_drop(lock);
}

/* queue_pop takes the oldest coroutine off a queue, or NULL. Taking it off is
   also what claims it for the thread that is about to run it, and the claim is
   a test: the word must still say a queue holds it, because only a coroutine a
   queue holds is one a thread may run. A second entry for a coroutine that
   another thread has already taken is dropped rather than run, so two threads
   can never share one stack. */
static zyxrt_coro *queue_pop(zyxrt_coro **head, zyxrt_coro **tail, zyxrt_lock *lock) {
  zyxrt_lock_hold(lock);
  zyxrt_coro *c = *head;
  if (c != NULL) {
    *head = c->wait_next;
    if (*head == NULL) {
      *tail = NULL;
    }
    c->wait_next = NULL;
  }
  zyxrt_lock_drop(lock);
  if (c != NULL) {
    unsigned word = atomic_fetch_and_explicit(&c->sched,
                                              ~(ZYXXRT_CORO_QUEUED | ZYXXRT_CORO_PARKED),
                                              memory_order_acq_rel);
    if ((word & ZYXXRT_CORO_QUEUED) == 0) return NULL;
  }
  return c;
}

/* sched_push puts a coroutine on a ready queue: the calling thread's own when
   it is a scheduler thread, and the handoff queue otherwise, which is what a
   thread outside the scheduler uses and what every thread uses once the
   program is over. */
static void sched_push(zyxrt_coro *c) {
  zyxrt_thread *self = self_thread;
  if (self != NULL && !atomic_load_explicit(&stopping, memory_order_acquire)) {
    queue_push(&self->head, &self->tail, &self->lock, c);
    return;
  }
  c->wait_next = NULL;
  zyxrt_lock_hold(&incoming_lock);
  if (incoming_tail != NULL) {
    incoming_tail->wait_next = c;
  } else {
    atomic_store_explicit(&incoming_head, c, memory_order_release);
  }
  incoming_tail = c;
  zyxrt_lock_drop(&incoming_lock);
  zyxrt_wakeup_signal();
}

/* sched_offer offers a coroutine to a ready queue. A coroutine a thread still
   has on its stack is not queued: the offer stays in the scheduling word, and
   the thread that takes the coroutine off its stack offers it again from
   there. The one that turns the offer into a queue entry takes the queue slot
   in the same compare-and-swap, so a second waker finds the slot taken. A
   coroutine that has finished is never queued: the region that started it
   releases it as soon as its finish is published, so a queue entry for it
   would point at storage that is already gone. */
static void sched_offer(zyxrt_coro *c) {
  unsigned word = atomic_load_explicit(&c->sched, memory_order_acquire);
  for (;;) {
    if (word & (ZYXXRT_CORO_QUEUED | ZYXXRT_CORO_DONE)) return;
    if (word & ZYXXRT_CORO_PARKED) {
      unsigned next = (word & ~ZYXXRT_CORO_WOKEN) | ZYXXRT_CORO_QUEUED;
      if (atomic_compare_exchange_weak_explicit(&c->sched, &word, next,
                                                memory_order_acq_rel, memory_order_acquire)) {
        sched_push(c);
        return;
      }
      continue;
    }
    unsigned next = word | ZYXXRT_CORO_WOKEN;
    if (atomic_compare_exchange_weak_explicit(&c->sched, &word, next,
                                              memory_order_acq_rel, memory_order_acquire)) {
      return;
    }
  }
}

void zyxrt_sched_ready(zyxrt_coro *c) {
  if (c == NULL) return;
  sched_offer(c);
}

/* incoming_ready reports whether work from outside the scheduler is waiting,
   which is a load rather than a lock. */
static int incoming_ready(void) {
  return atomic_load_explicit(&incoming_head, memory_order_acquire) != NULL;
}

/* incoming_pop takes one coroutine off the queue work from outside lands on. */
static zyxrt_coro *incoming_pop(void) {
  zyxrt_lock_hold(&incoming_lock);
  zyxrt_coro *c = atomic_load_explicit(&incoming_head, memory_order_relaxed);
  if (c != NULL) {
    atomic_store_explicit(&incoming_head, c->wait_next, memory_order_release);
    if (c->wait_next == NULL) incoming_tail = NULL;
    c->wait_next = NULL;
  }
  zyxrt_lock_drop(&incoming_lock);
  if (c != NULL) {
    unsigned word = atomic_fetch_and_explicit(&c->sched,
                                              ~(ZYXXRT_CORO_QUEUED | ZYXXRT_CORO_PARKED),
                                              memory_order_acq_rel);
    if ((word & ZYXXRT_CORO_QUEUED) == 0) return NULL;
  }
  return c;
}

/* take_ready returns the next coroutine this thread should run: work that
   arrived from another thread first, because a thread with a queue of its own
   would otherwise never look at it, then its own next, then a share of what
   another thread has left. Stealing is what keeps one busy thread from
   leaving the others idle. */
static zyxrt_coro *take_ready(zyxrt_thread *t) {
  if (incoming_ready()) {
    zyxrt_coro *c = incoming_pop();
    if (c != NULL) return c;
  }
  zyxrt_coro *c = queue_pop(&t->head, &t->tail, &t->lock);
  if (c != NULL) return c;
  c = incoming_pop();
  if (c != NULL) return c;
  for (size_t i = 1; i < thread_count; i++) {
    zyxrt_thread *other = &threads[(t->index + i) % thread_count];
    c = queue_pop(&other->head, &other->tail, &other->lock);
    if (c != NULL) return c;
  }
  return NULL;
}

zyxrt_coro *zyxrt_sched_current(void) {
  return self_thread == NULL ? NULL : self_thread->current;
}

void zyxrt_sched_suspend(zyxrt_coro *c) {
  zyxrt_thread *t = self_thread;
  if (t == NULL) return;
  zyxrt_ctx_switch(c->ctx, t->ctx);
}

/* sched_finish publishes a coroutine that has finished. It runs on the thread
   that had the coroutine on its stack, after the switch back, and it is the
   last thing that touches the coroutine: the region that started it releases
   it as soon as it sees it done. */
static void sched_finish(zyxrt_coro *c) {
  zyxrt_lock_hold(&c->lock);
  c->done = 1;
  zyxrt_wait_wake(&c->waiters);
  zyxrt_lock_drop(&c->lock);
}

/* run_once runs one ready coroutine until it suspends or finishes. It answers
   whether there was one. */
static int run_once(zyxrt_thread *t) {
  zyxrt_coro *c = take_ready(t);
  if (c == NULL) return 0;
  t->current = c;
  /* The monitor watches the coroutine the scheduler is running, so it is
     recorded before the switch and cleared after it. */
  zyxrt_preempt_enter(c);
  zyxrt_ctx_switch(t->ctx, c->ctx);
  zyxrt_preempt_leave();
  t->current = NULL;
  /* The coroutine is off this thread's stack now, and this thread is the one
     that says what becomes of it. A waker that asked for it while it was
     running could not queue it; it is queued here instead. Both decisions
     belong to this thread for the same reason: while the stack was in use, a
     queue entry for it would have let another thread run it. A coroutine that
     finished is marked done in the same step as parking it, so a waker that
     arrives after this point finds it done rather than queued, and the finish
     that follows may release it at once. */
  unsigned park = ZYXXRT_CORO_PARKED | (c->exit ? ZYXXRT_CORO_DONE : 0u);
  unsigned word = atomic_fetch_or_explicit(&c->sched, park, memory_order_acq_rel);
  if (c->exit) {
    sched_finish(c);
    return 1;
  }
  if (word & ZYXXRT_CORO_WOKEN) sched_offer(c);
  return 1;
}

/* zyxrt_sched_enter runs on a coroutine's own stack. It owns a region, runs
   the body, releases the region, and returns to the scheduler. The finish is
   left to the scheduler thread that takes this stack off, which publishes it
   only once nothing reads the stack any more. */
void zyxrt_sched_enter(void) {
  zyxrt_coro *c = zyxrt_sched_current();
  c->region = zyxrt_region_new();
  c->fn(c->arg);
  zyxrt_region_free(c->region);
  c->region = NULL;
  c->exit = 1;
  zyxrt_thread *t = self_thread;
  zyxrt_ctx_switch(c->ctx, t->ctx);
}

/* zyxrt_sched_abandon ends the running coroutine and returns to the
   scheduler without returning into the coroutine's stack. A panic uses it:
   the frames above the raise point are left where they are, because the
   coroutine's region is released as a whole. */
void zyxrt_sched_abandon(zyxrt_coro *c) {
  /* The supervisor sees the exception before the region that holds it is
     released, because the exception lives in that region. */
  if (c->watch != NULL) {
    c->watch(c->watch_recv, c->panic);
    c->watch = NULL;
  }
  if (c->region != NULL) {
    zyxrt_region_free(c->region);
    c->region = NULL;
  }
  c->exit = 1;
  zyxrt_thread *t = self_thread;
  zyxrt_ctx_switch(c->ctx, t->ctx);
}

void zyxrt_sched_park(uint64_t deadline) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  /* The timer lives on this frame, which stays alive while the coroutine is
     parked, so a deadline costs no allocation. */
  zyxrt_timer t;
  t.coro = self;
  zyxrt_wheel_add(&t, deadline);
  zyxrt_sched_suspend(self);
  /* A deadline the wheel already removed is removed again, which does
     nothing; one that did not fire leaves the wheel here. */
  zyxrt_wheel_remove(&t);
}

void zyxrt_sched_sleep(uint64_t ns) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  zyxrt_sched_park(zyxrt_now() + ns);
}

/* zyxrt_sched_advance takes in what the event loop found and fires every
   deadline that has come due, so the run that follows sees everything that is
   ready now. */
void zyxrt_sched_advance(void) {
  zyxrt_loop_drain();
  zyxrt_wheel_advance(zyxrt_now());
}

/* sched_idle_wait answers how long a thread with nothing to run may sleep:
   the distance to the next deadline, -1 when the wheel holds none, or
   ZYXRT_WAIT_EXTERNAL when a coroutine also waits for something the scheduler
   cannot see. A wait of -1 therefore means that neither is outstanding. The
   caller holds state_lock, which is what makes the answer worth having: a
   deadline added after it belongs to a thread that has already gone idle, so
   that thread wakes this one instead of being missed. */
static int64_t sched_idle_wait(void) {
  int64_t wait = zyxrt_wheel_next(zyxrt_now());
  if (wait < 0 && atomic_load_explicit(&externals, memory_order_acquire) > 0) {
    return ZYXRT_WAIT_EXTERNAL;
  }
  return wait;
}

/* sched_sleep sleeps until a deadline, until work arrives, or until a
   descriptor becomes ready. A target without a poller thread of its own sleeps
   in the poll, because it has no thread to be woken by. */
static void sched_sleep(int64_t wait) {
  if (zyxrt_loop_poll_once(wait)) return;
  if (wait < 0) {
    /* Nothing is waiting, so any arrival is a reason to look again. */
    zyxrt_wakeup_wait(-1);
  } else if (wait > 0) {
    zyxrt_wakeup_wait(wait);
  }
}

void zyxrt_sched_yield(void) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  zyxrt_sched_ready(self);
  zyxrt_sched_suspend(self);
}

/* The wait lists. A caller holds the lock of the state it looked at; these
   only link and unlink, and wake readies what it takes off the list. */
void zyxrt_wait_add(zyxrt_waitlist *list, zyxrt_waiter *w) {
  w->next = list->head;
  list->head = w;
}

void zyxrt_wait_remove(zyxrt_waitlist *list, zyxrt_waiter *w) {
  zyxrt_waiter **link = &list->head;
  while (*link) {
    if (*link == w) {
      *link = w->next;
      return;
    }
    link = &(*link)->next;
  }
}

void zyxrt_wait_wake(zyxrt_waitlist *list) {
  zyxrt_waiter *w = list->head;
  list->head = NULL;
  while (w) {
    zyxrt_waiter *next = w->next;
    zyxrt_sched_ready(w->coro);
    w = next;
  }
}

/* free_coro releases a finished coroutine. */
static void free_coro(zyxrt_coro *c) {
  /* The region that was waiting for this coroutine must not wait for it
     again. */
  zyxrt_region_detach(c->owner_region, c);
  if (c->ctx) zyxrt_ctx_free(c->ctx);
  if (c->stack) zyxrt_stack_unmap(c->stack, c->stack_size);
  free(c->regions);
  free(c);
}

void *zyxrt_coro_spawn(zyxrt_fn fn, void *arg) {
  return zyxrt_coro_spawn_watched(fn, arg, NULL, NULL);
}

void *zyxrt_coro_spawn_watched(zyxrt_fn fn, void *arg, zyxrt_watch_fn watch, void *watch_recv) {
  zyxrt_coro *c = (zyxrt_coro *)calloc(1, sizeof(zyxrt_coro));
  if (!c) zyxrt_panic("out of memory", 13);
  zyxrt_lock_init(&c->lock);
  c->fn = fn;
  c->arg = arg;
  c->watch = watch;
  c->watch_recv = watch_recv;
  c->stack_size = zyxrt_stack_size();
  c->stack = zyxrt_stack_map(c->stack_size);
  if (!c->stack) zyxrt_panic("out of memory", 13);
  zyxrt_stack_warm(c->stack, c->stack_size);
  c->ctx = zyxrt_ctx_new(c->stack, c->stack_size);
  /* The region the spawner is running in waits for this coroutine before it
     releases its memory, so the coroutine never reads freed storage. */
  zyxrt_region_adopt(zyxrt_region_current(), c);
  /* No thread has this one on its stack yet, so a waker may queue it at once
     rather than leave the offer for a thread to pick up. */
  atomic_store_explicit(&c->sched, ZYXXRT_CORO_PARKED, memory_order_relaxed);
  zyxrt_sched_ready(c);
  return c;
}

void *zyxrt_coro_join(void *handle, void **panic) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (panic) *panic = NULL;
  if (!c) return NULL;
  zyxrt_lock_hold(&c->lock);
  while (!c->done) {
    /* The joiner blocks on the coroutine and is woken when it finishes.
       Suspending without joining the wait list would leave nobody to run
       the joiner again. */
    if (self == NULL) break;
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&c->waiters, &w);
    zyxrt_lock_drop(&c->lock);
    zyxrt_sched_suspend(self);
    zyxrt_lock_hold(&c->lock);
  }
  /* The coroutine stays where it is: the region that adopted it releases it,
     so joining a second time answers the same way. */
  void *result = c->result;
  if (panic) *panic = c->panic;
  zyxrt_lock_drop(&c->lock);
  return result;
}

int zyxrt_coro_try_join(void *handle, void **result, void **panic) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (panic) *panic = NULL;
  if (!c) return 0;
  zyxrt_lock_hold(&c->lock);
  int done = c->done;
  if (done) {
    if (result) *result = c->result;
    if (panic) *panic = c->panic;
  }
  zyxrt_lock_drop(&c->lock);
  return done;
}

void zyxrt_coro_set_result(void *result) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (!c) return;
  zyxrt_lock_hold(&c->lock);
  c->result = result;
  zyxrt_lock_drop(&c->lock);
}

/* zyxrt_region_wait blocks the running coroutine until every coroutine
   started in the region has finished. A region releases its memory after
   this, so nothing it owns is read once it is gone. */
void zyxrt_region_wait(void *region) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (self == NULL) {
    return;
  }
  for (;;) {
    zyxrt_coro *child = (zyxrt_coro *)zyxrt_region_take_child(region);
    if (child == NULL) {
      return;
    }
    zyxrt_lock_hold(&child->lock);
    while (!child->done) {
      /* The joiner blocks on the child and is woken when it finishes. */
      zyxrt_waiter w = {self, NULL, 0};
      zyxrt_wait_add(&child->waiters, &w);
      zyxrt_lock_drop(&child->lock);
      zyxrt_sched_suspend(self);
      zyxrt_lock_hold(&child->lock);
    }
    zyxrt_lock_drop(&child->lock);
    free_coro(child);
  }
}

/* zyxrt_coro_panic returns the exception a coroutine raised, or NULL. */
void *zyxrt_coro_panic(void *handle) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (!c) return NULL;
  return c->panic;
}

/* scheduler_threads returns how many threads the scheduler runs. ZYX_THREADS
   names it, and the scheduler runs one when it says nothing: a coroutine that
   blocks then suspends while the next one runs, and the program's own thread
   runs coroutines rather than watching them. */
static size_t scheduler_threads(void) {
  const char *set = getenv("ZYX_THREADS");
  if (set != NULL && set[0] != '\0') {
    long n = strtol(set, NULL, 10);
    if (n > 0) {
      return (size_t)n > ZYXXRT_PREEMPT_SLOTS ? ZYXXRT_PREEMPT_SLOTS : (size_t)n;
    }
  }
  return 1;
}

/* thread_main is where a scheduler thread begins. */
static void thread_main(void *arg) {
  zyxrt_thread *t = (zyxrt_thread *)arg;
  self_thread = t;
  t->ctx = zyxrt_ctx_thread();
  zyxrt_preempt_thread();
  zyxrt_sched_run();
}

/* zyxrt_sched_run is the scheduler loop of the calling thread: take in what
   arrived, run what is ready, and stop when every thread has nothing left to do
   and nothing is on its way. */
void zyxrt_sched_run(void) {
  zyxrt_thread *t = self_thread;
  for (;;) {
    zyxrt_sched_advance();
    if (run_once(t)) continue;
    /* Nothing to run here. Two threads may arrive at this at once, so the
       decision is made under the lock, and the evidence for it is read under
       the same lock: a deadline added after the wait was computed belongs to a
       thread that has already gone idle, and deciding the program is over on
       the strength of the earlier reading would leave that deadline to fire
       with nobody to run it. Reading the wheel here instead cannot race with a
       thread that is about to park, because a thread parks before it goes
       idle. */
    zyxrt_lock_hold(&state_lock);
    idle_threads++;
    int64_t wait = sched_idle_wait();
    int over = idle_threads == thread_count && wait == -1 && !incoming_ready();
    if (over) {
      atomic_store_explicit(&stopping, 1, memory_order_release);
    }
    zyxrt_lock_drop(&state_lock);
    if (over) {
      /* Every other thread is waiting on the wakeup handle and nothing will
         wake it, so each one is released; each of them then finds the program
         over and returns. */
      zyxrt_wakeup_signal_all(thread_count - 1);
      return;
    }
    sched_sleep(wait);
    zyxrt_lock_hold(&state_lock);
    idle_threads--;
    zyxrt_lock_drop(&state_lock);
    if (atomic_load_explicit(&stopping, memory_order_acquire)) return;
  }
}

void zyxrt_sched_init(void) {
  zyxrt_lock_init(&incoming_lock);
  zyxrt_lock_init(&state_lock);
  idle_threads = 0;
  atomic_store_explicit(&stopping, 0, memory_order_relaxed);
  atomic_store_explicit(&externals, 0, memory_order_relaxed);
  incoming_head = NULL;
  incoming_tail = NULL;
  /* A target without threads runs one, whatever ZYX_THREADS says. */
  thread_count = zyxrt_threads() ? scheduler_threads() : 1;
  threads = (zyxrt_thread *)calloc(thread_count, sizeof(zyxrt_thread));
  if (!threads) zyxrt_panic("out of memory", 13);
  for (size_t i = 0; i < thread_count; i++) {
    threads[i].head = threads[i].tail = NULL;
    threads[i].current = NULL;
    threads[i].index = i;
    zyxrt_lock_init(&threads[i].lock);
  }
  /* The calling thread is the first of them, so the program's own thread runs
     coroutines rather than sitting outside the scheduler. */
  self_thread = &threads[0];
  threads[0].ctx = zyxrt_ctx_thread();
  zyxrt_preempt_thread();
  for (size_t i = 1; i < thread_count; i++) {
    zyxrt_thread_start(thread_main, &threads[i]);
  }
}

/* zyxrt_sched_shutdown wakes every thread that is waiting so the process can
   end. A thread that is between coroutines is idle by then, so waking it is a
   courtesy rather than a requirement. */
void zyxrt_sched_shutdown(void) {
  atomic_store_explicit(&stopping, 1, memory_order_release);
  zyxrt_wakeup_signal_all(thread_count - 1);
}
