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
  return c;
}

/* coro_entry runs on a coroutine's own stack. It owns a region, runs the
   body, releases the region, and returns to the scheduler. */
static void coro_entry(void) {
  zyxrt_coro *c = current;
  c->region = zyxrt_region_new();
  c->fn(c->arg);
  zyxrt_region_free(c->region);
  c->region = NULL;
  c->done = 1;
  current = NULL;
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

void zyxrt_sched_yield(void) {
  zyxrt_coro *self = current;
  if (!self) return;
  zyxrt_sched_ready(self);
  zyxrt_sched_suspend(self);
}

void *zyxrt_coro_spawn(zyxrt_fn fn, void *arg) {
  zyxrt_coro *c = (zyxrt_coro *)calloc(1, sizeof(zyxrt_coro));
  if (!c) zyxrt_panic("out of memory", 13);
  c->fn = fn;
  c->arg = arg;
  c->stack_size = ZYXXRT_STACK_MIN;
  c->stack = malloc(c->stack_size);
  if (!c->stack) zyxrt_panic("out of memory", 13);
  c->ctx = zyxrt_ctx_new(c->stack, c->stack_size, coro_entry);
  zyxrt_sched_ready(c);
  return c;
}

/* free_coro releases a finished coroutine. */
static void free_coro(zyxrt_coro *c) {
  if (c->ctx) zyxrt_ctx_free(c->ctx);
  free(c->stack);
  free(c);
}

void *zyxrt_coro_join(void *handle) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (!c) return NULL;
  while (!c->done) {
    zyxrt_sched_suspend(c);
  }
  void *result = c->result;
  free_coro(c);
  return result;
}

int zyxrt_coro_try_join(void *handle, void **result) {
  zyxrt_coro *c = (zyxrt_coro *)handle;
  if (!c || !c->done) return 0;
  if (result) *result = c->result;
  free_coro(c);
  return 1;
}

void zyxrt_sched_wake(zyxrt_coro **list) {
  zyxrt_coro *c = *list;
  *list = NULL;
  while (c) {
    zyxrt_coro *next = c->wait_next;
    zyxrt_sched_ready(c);
    c = next;
  }
}
