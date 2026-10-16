/* Cooperative coroutine scheduler. Coroutines run on their own stacks; a
   coroutine that cannot proceed suspends itself and the scheduler resumes
   the next ready one. */

#include "zyxrt_internal.h"
#include "thread.h"

#include <stdlib.h>

/* The ready queue, as a singly linked list. */
static zyxrt_coro *ready_head;
static zyxrt_coro *ready_tail;
static zyxrt_coro *current;
static void *sched_ctx;

void zyxrt_sched_init(void) {
  ready_head = ready_tail = current = NULL;
  sched_ctx = zyxrt_ctx_thread();
  zyxrt_sched_post_init();
}

void zyxrt_sched_shutdown(void) {
  ready_head = ready_tail = current = NULL;
}

zyxrt_coro *zyxrt_sched_current(void) { return current; }

void zyxrt_sched_ready(zyxrt_coro *c) {
  if (c->queued) {
    /* A select waits on several lists, so more than one of them can wake it.
       The queue holds it once. */
    return;
  }
  c->queued = 1;
  c->wait_next = NULL;
  if (ready_tail) {
    ready_tail->wait_next = c;
  } else {
    ready_head = c;
  }
  ready_tail = c;
}

/* posted holds the coroutines another thread made ready, and post_lock guards
   it. A pool thread or a poller thread appends to the list and signals the
   wakeup handle; the scheduler drains the list on its own thread. The head is
   atomic so the scheduler can tell that the list is empty without taking the
   lock, which is what makes draining on every turn affordable. */
static zyxrt_lock post_lock;
static _Atomic(zyxrt_coro *) post_head;
static int post_ready;

void zyxrt_sched_post_init(void) {
  zyxrt_lock_init(&post_lock);
  post_ready = 1;
}

void zyxrt_sched_post(zyxrt_coro *c) {
  if (!post_ready) {
    /* Without the lock there is no other thread to race with. */
    zyxrt_sched_ready(c);
    return;
  }
  zyxrt_lock_hold(&post_lock);
  c->post_next = atomic_load_explicit(&post_head, memory_order_relaxed);
  atomic_store_explicit(&post_head, c, memory_order_release);
  zyxrt_lock_drop(&post_lock);
  zyxrt_wakeup_signal();
}

int zyxrt_sched_drain(void) {
  if (!post_ready) return 0;
  if (atomic_load_explicit(&post_head, memory_order_acquire) == NULL) return 0;
  int readied = 0;
  for (;;) {
    zyxrt_lock_hold(&post_lock);
    zyxrt_coro *c = atomic_load_explicit(&post_head, memory_order_relaxed);
    atomic_store_explicit(&post_head, NULL, memory_order_relaxed);
    zyxrt_lock_drop(&post_lock);
    if (c == NULL) return readied;
    while (c != NULL) {
      zyxrt_coro *next = c->post_next;
      zyxrt_sched_ready(c);
      c = next;
      readied = 1;
    }
  }
}

/* take_ready removes the first ready coroutine. */
static zyxrt_coro *take_ready(void) {
  zyxrt_coro *c = ready_head;
  if (!c) return NULL;
  ready_head = c->wait_next;
  if (!ready_head) ready_tail = NULL;
  c->wait_next = NULL;
  c->queued = 0;
  return c;
}

/* zyxrt_sched_enter runs on a coroutine's own stack. It owns a region, runs
   the body, releases the region, and returns to the scheduler. */
void zyxrt_sched_enter(void) {
  zyxrt_coro *c = current;
  c->region = zyxrt_region_new();
  c->fn(c->arg);
  zyxrt_region_free(c->region);
  c->region = NULL;
  c->done = 1;
  current = NULL;
  zyxrt_wait_wake(&c->waiters);
  zyxrt_ctx_switch(c->ctx, sched_ctx);
}

int zyxrt_sched_run_once(void) {
  zyxrt_coro *c = take_ready();
  if (!c) return 0;
  current = c;
  /* The monitor watches the coroutine the scheduler is running, so it is
     recorded before the switch and cleared after it. */
  zyxrt_preempt_enter(c);
  zyxrt_ctx_switch(sched_ctx, c->ctx);
  zyxrt_preempt_leave();
  current = NULL;
  return 1;
}

void zyxrt_sched_suspend(zyxrt_coro *c) {
  zyxrt_ctx_switch(c->ctx, sched_ctx);
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
  c->done = 1;
  current = NULL;
  zyxrt_wait_wake(&c->waiters);
  zyxrt_ctx_switch(c->ctx, sched_ctx);
}

/* sleepers held the waiters of the deadlines; the wheel holds them now, so
   the only state left here is what counts a coroutine waiting for something
   the scheduler cannot see. */

/* externals counts the coroutines waiting for something outside the
   scheduler, which is what tells the main loop to wait on the wakeup handle
   rather than ending the program. */
static int externals;

void zyxrt_sched_external(int delta) { externals += delta; }

void zyxrt_sched_park(uint64_t deadline) {
  zyxrt_coro *self = current;
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
  zyxrt_coro *self = current;
  if (!self) return;
  zyxrt_sched_park(zyxrt_now() + ns);
}

int64_t zyxrt_sched_wait(void) {
  uint64_t now = zyxrt_now();
  zyxrt_wheel_advance(now);
  int64_t wait = zyxrt_wheel_next(now);
  /* A target whose event loop has no poller thread watches the descriptors
     here, so one wait covers both the deadlines and the descriptors. */
  if (externals > 0 && zyxrt_loop_poll_once(wait)) {
    return 0;
  }
  if (wait < 0 && externals > 0) {
    /* Nothing is due, but a coroutine waits for something the scheduler
       cannot see, so the loop waits on the wakeup handle. */
    return ZYXRT_WAIT_EXTERNAL;
  }
  return wait;
}

void zyxrt_sched_yield(void) {
  zyxrt_coro *self = current;
  if (!self) return;
  zyxrt_sched_ready(self);
  zyxrt_sched_suspend(self);
}

void *zyxrt_coro_spawn(zyxrt_fn fn, void *arg) {
  return zyxrt_coro_spawn_watched(fn, arg, NULL, NULL);
}

void *zyxrt_coro_spawn_watched(zyxrt_fn fn, void *arg, zyxrt_watch_fn watch, void *watch_recv) {
  zyxrt_coro *c = (zyxrt_coro *)calloc(1, sizeof(zyxrt_coro));
  if (!c) zyxrt_panic("out of memory", 13);
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
  zyxrt_sched_ready(c);
  return c;
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

void *zyxrt_coro_join(void *handle, void **panic) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (panic) *panic = NULL;
  if (!c) return NULL;
  while (!c->done) {
    /* The joiner blocks on the coroutine and is woken when it finishes.
       Suspending without joining the wait list would leave nobody to run
       the joiner again. */
    zyxrt_coro *self = current;
    if (!self) break;
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&c->waiters, &w);
    zyxrt_sched_suspend(self);
  }
  /* The coroutine stays where it is: the region that adopted it releases it,
     so joining a second time answers the same way. */
  if (panic) *panic = c->panic;
  return c->result;
}

int zyxrt_coro_try_join(void *handle, void **result, void **panic) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (panic) *panic = NULL;
  if (!c || !c->done) return 0;
  if (result) *result = c->result;
  if (panic) *panic = c->panic;
  return 1;
}

void zyxrt_coro_set_result(void *result) {
  if (current) current->result = result;
}

/* zyxrt_region_wait blocks the running coroutine until every coroutine
   started in the region has finished. A region releases its memory after
   this, so nothing it owns is read once it is gone. */
void zyxrt_region_wait(void *region) {
  zyxrt_coro *self = current;
  if (self == NULL) {
    return;
  }
  for (;;) {
    zyxrt_coro *child = (zyxrt_coro *)zyxrt_region_take_child(region);
    if (child == NULL) {
      return;
    }
    while (!child->done) {
      /* The joiner blocks on the child and is woken when it finishes. */
      zyxrt_waiter w = {self, NULL};
      zyxrt_wait_add(&child->waiters, &w);
      zyxrt_sched_suspend(self);
    }
    free_coro(child);
  }
}

/* The wait lists. A waiter puts a node on a list and suspends; a waker
   readies every coroutine of a list and empties it. */
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

/* zyxrt_coro_panic returns the exception a coroutine raised, or NULL. */
void *zyxrt_coro_panic(void *handle) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (!c) return NULL;
  return c->panic;
}
