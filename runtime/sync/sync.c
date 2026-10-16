/* Atomics, fences, and sync primitive support. A primitive that must wait
   parks the calling coroutine on its own wait list, so waiting costs no
   scheduler thread. */

#include "zyxrt_internal.h"


/* A wait group counts the coroutines a waiter waits for. */
typedef struct zyxrt_wg {
  int64_t count;
  zyxrt_waitlist waiters;
} zyxrt_wg;

void *zyxrt_wg_new(void) {
  zyxrt_wg *wg = (zyxrt_wg *)zyxrt_region_alloc_current(sizeof(zyxrt_wg), 8);
  wg->count = 0;
  wg->waiters.head = NULL;
  return wg;
}

void zyxrt_wg_add(void *handle, int64_t n) {
  zyxrt_wg *wg = (zyxrt_wg *)handle;
  wg->count += n;
  /* A group that has reached zero lets its waiters go; a group that is
     counted up again simply waits for the next time. */
  if (wg->count <= 0) zyxrt_wait_wake(&wg->waiters);
}

void zyxrt_wg_wait(void *handle) {
  zyxrt_wg *wg = (zyxrt_wg *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  while (wg->count > 0) {
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&wg->waiters, &w);
    zyxrt_sched_suspend(self);
  }
}

/* A once runs one function exactly once. The first caller runs it and the
   others wait for it to finish, which is what makes the value the function
   builds visible to every caller. */
typedef struct zyxrt_once {
  int done;
  zyxrt_waitlist waiters;
} zyxrt_once;

void *zyxrt_once_new(void) {
  zyxrt_once *o = (zyxrt_once *)zyxrt_region_alloc_current(sizeof(zyxrt_once), 8);
  o->done = 0;
  o->waiters.head = NULL;
  return o;
}

int zyxrt_once_enter(void *handle) {
  zyxrt_once *o = (zyxrt_once *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (self) {
    /* Another caller is running the function, so wait for it to finish: what
       it builds must be visible before anyone returns. */
    while (o->done == 1) {
      zyxrt_waiter w = {self, NULL};
      zyxrt_wait_add(&o->waiters, &w);
      zyxrt_sched_suspend(self);
    }
  }
  if (o->done == 2) return 0;
  /* The caller that gets here runs the function. */
  o->done = 1;
  return 1;
}

void zyxrt_once_leave(void *handle) {
  zyxrt_once *o = (zyxrt_once *)handle;
  o->done = 2;
  zyxrt_wait_wake(&o->waiters);
}

/* A mutex is held by one coroutine at a time. Waiting parks the coroutine,
   so a lock that is contended costs no scheduler thread, and a monitor
   method releases the lock while it waits, as a condition variable does. */
typedef struct zyxrt_mutex {
  int locked;
  zyxrt_waitlist waiters;
  zyxrt_waitlist monitors;
} zyxrt_mutex;

void *zyxrt_mutex_new(void) {
  zyxrt_mutex *m = (zyxrt_mutex *)zyxrt_region_alloc_current(sizeof(zyxrt_mutex), 8);
  m->locked = 0;
  m->waiters.head = NULL;
  m->monitors.head = NULL;
  return m;
}

void zyxrt_mutex_lock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  while (m->locked) {
    if (!self) return;
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&m->waiters, &w);
    zyxrt_sched_suspend(self);
  }
  m->locked = 1;
}

int zyxrt_mutex_try_lock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  if (m->locked) return 0;
  m->locked = 1;
  return 1;
}

void zyxrt_mutex_unlock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  m->locked = 0;
  if (m->waiters.head) zyxrt_wait_wake(&m->waiters);
}

/* zyxrt_mutex_wait releases the lock and parks the coroutine until a monitor
   notification arrives, then takes the lock again. The caller must hold the
   lock. */
void zyxrt_mutex_wait(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  m->locked = 0;
  if (m->waiters.head) zyxrt_wait_wake(&m->waiters);
  if (self) {
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&m->monitors, &w);
    zyxrt_sched_suspend(self);
  }
  zyxrt_mutex_lock(handle);
}

void zyxrt_mutex_notify_one(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_waiter *w = m->monitors.head;
  if (!w) return;
  m->monitors.head = w->next;
  zyxrt_sched_ready(w->coro);
}

void zyxrt_mutex_notify_all(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_wait_wake(&m->monitors);
}

/* A semaphore holds a count of permits. */
typedef struct zyxrt_sem {
  int64_t count;
  zyxrt_waitlist waiters;
} zyxrt_sem;

void *zyxrt_sem_new(int64_t n) {
  zyxrt_sem *s = (zyxrt_sem *)zyxrt_region_alloc_current(sizeof(zyxrt_sem), 8);
  s->count = n;
  s->waiters.head = NULL;
  return s;
}

void zyxrt_sem_acquire(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  while (s->count <= 0) {
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&s->waiters, &w);
    zyxrt_sched_suspend(self);
  }
  s->count--;
}

int zyxrt_sem_try_acquire(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  if (s->count <= 0) return 0;
  s->count--;
  return 1;
}

void zyxrt_sem_release(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  s->count++;
  if (s->waiters.head) zyxrt_wait_wake(&s->waiters);
}
