/* Cooperative coroutine scheduler. Coroutines run on their own stacks; a
   coroutine that cannot proceed suspends itself and the scheduler resumes
   the next ready one. */

#include "zyxrt_internal.h"

#include <stdlib.h>

/* The ready queue, as a singly linked list. */
static zyxrt_coro *ready_head;
static zyxrt_coro *ready_tail;
static zyxrt_coro *current;
static void *sched_ctx;

void zyxrt_sched_init(void) {
  ready_head = ready_tail = current = NULL;
  sched_ctx = zyxrt_ctx_thread();
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
  zyxrt_ctx_switch(sched_ctx, c->ctx);
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

/* sleepers holds the waiters of the deadlines. */
static zyxrt_waitlist sleepers;

zyxrt_waitlist *zyxrt_sched_sleepers(void) { return &sleepers; }

void zyxrt_sched_park(uint64_t deadline) {
  zyxrt_coro *self = current;
  if (!self) return;
  self->wake_at = deadline;
  zyxrt_waiter w = {self, NULL};
  zyxrt_wait_add(&sleepers, &w);
  zyxrt_sched_suspend(self);
}

void zyxrt_sched_sleep(uint64_t ns) {
  zyxrt_coro *self = current;
  if (!self) return;
  zyxrt_sched_park(zyxrt_now() + ns);
}

int64_t zyxrt_sched_wait(void) {
  uint64_t now = zyxrt_now();
  int64_t wait = -1;
  int readied = 0;
  zyxrt_waiter **link = &sleepers.head;
  while (*link) {
    zyxrt_waiter *w = *link;
    int64_t remaining = (int64_t)(w->coro->wake_at - now);
    if (remaining <= 0) {
      *link = w->next;
      zyxrt_sched_ready(w->coro);
      readied = 1;
      continue;
    }
    if (wait < 0 || remaining < wait) {
      wait = remaining;
    }
    link = &w->next;
  }
  /* A coroutine that just became ready must run before the scheduler waits
     for anything else, so the caller is told to run again rather than to
     sleep. */
  if (readied) {
    return 0;
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
