/* Deferred calls. A coroutine owns a stack of pending calls. A region
   records a mark when it is entered and runs the calls above that mark when
   it leaves, in reverse order, which is the order the language prescribes.
   A panic runs the whole stack, because a panic leaves every enclosing
   region and every deferred call therefore belongs to a region that is
   leaving. */

#include "zyxrt_internal.h"

#include <stdlib.h>

/* zyxrt_defer_mark returns the current top of the coroutine's defer stack,
   which is what a region records on entry. */
void *zyxrt_defer_mark(void) {
  zyxrt_coro *c = zyxrt_sched_current();
  return c == NULL ? NULL : c->defers;
}

/* zyxrt_defer_push records a call to run when the region leaves. The record
   lives in the region, so it is released with everything else. */
void zyxrt_defer_push(void (*call)(void *), void *args) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (c == NULL) {
    zyxrt_panic("a deferred call outside a coroutine", 34);
  }
  zyxrt_defer *d = (zyxrt_defer *)zyxrt_region_alloc_current(sizeof(zyxrt_defer), sizeof(void *));
  d->call = call;
  d->args = args;
  d->next = c->defers;
  c->defers = d;
}

/* zyxrt_defer_run runs the calls recorded since a mark, in reverse order.
   A mark of NULL runs everything. */
void zyxrt_defer_run(void *mark) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (c == NULL) {
    return;
  }
  zyxrt_defer *d = c->defers;
  while (d != NULL && d != (zyxrt_defer *)mark) {
    zyxrt_defer *next = d->next;
    c->defers = next;
    d->call(d->args);
    d = next;
  }
  c->defers = (zyxrt_defer *)mark;
}
