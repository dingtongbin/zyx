/* Sync primitives. A primitive that must wait parks the calling coroutine on
   its own wait list, so waiting costs no scheduler thread. */

#include "zyxrt_internal.h"

#include <stdatomic.h>

/* An atomic cell is allocated as the C atomic type of its width, so every
   access is an atomic access to an atomic object rather than a cast of one.
   The language's memory order maps to the C one. */
typedef struct zyxrt_atomic {
  size_t size;
  union {
    _Atomic uint8_t b8;
    _Atomic uint16_t b16;
    _Atomic uint32_t b32;
    _Atomic uint64_t b64;
  } cell;
} zyxrt_atomic;

/* The order numbers are the ones the language's Order enum uses. */
static memory_order order_of(int64_t order) {
  switch (order) {
  case 1:
    return memory_order_relaxed;
  case 2:
    return memory_order_acquire;
  case 3:
    return memory_order_release;
  case 4:
    return memory_order_acq_rel;
  default:
    return memory_order_seq_cst;
  }
}

void *zyxrt_atomic_new(int64_t size, uint64_t value) {
  zyxrt_atomic *a = (zyxrt_atomic *)zyxrt_region_alloc_current(sizeof(zyxrt_atomic), 8);
  a->size = (size_t)size;
  switch (a->size) {
  case 1:
    atomic_init(&a->cell.b8, (uint8_t)value);
    break;
  case 2:
    atomic_init(&a->cell.b16, (uint16_t)value);
    break;
  case 4:
    atomic_init(&a->cell.b32, (uint32_t)value);
    break;
  default:
    atomic_init(&a->cell.b64, value);
    break;
  }
  return a;
}

uint64_t zyxrt_atomic_load(void *handle, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  switch (a->size) {
  case 1:
    return atomic_load_explicit(&a->cell.b8, order_of(order));
  case 2:
    return atomic_load_explicit(&a->cell.b16, order_of(order));
  case 4:
    return atomic_load_explicit(&a->cell.b32, order_of(order));
  default:
    return atomic_load_explicit(&a->cell.b64, order_of(order));
  }
}

void zyxrt_atomic_store(void *handle, uint64_t value, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  switch (a->size) {
  case 1:
    atomic_store_explicit(&a->cell.b8, (uint8_t)value, order_of(order));
    break;
  case 2:
    atomic_store_explicit(&a->cell.b16, (uint16_t)value, order_of(order));
    break;
  case 4:
    atomic_store_explicit(&a->cell.b32, (uint32_t)value, order_of(order));
    break;
  default:
    atomic_store_explicit(&a->cell.b64, value, order_of(order));
    break;
  }
}

uint64_t zyxrt_atomic_add(void *handle, uint64_t value, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  switch (a->size) {
  case 1:
    return atomic_fetch_add_explicit(&a->cell.b8, (uint8_t)value, order_of(order));
  case 2:
    return atomic_fetch_add_explicit(&a->cell.b16, (uint16_t)value, order_of(order));
  case 4:
    return atomic_fetch_add_explicit(&a->cell.b32, (uint32_t)value, order_of(order));
  default:
    return atomic_fetch_add_explicit(&a->cell.b64, value, order_of(order));
  }
}

uint64_t zyxrt_atomic_sub(void *handle, uint64_t value, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  switch (a->size) {
  case 1:
    return atomic_fetch_sub_explicit(&a->cell.b8, (uint8_t)value, order_of(order));
  case 2:
    return atomic_fetch_sub_explicit(&a->cell.b16, (uint16_t)value, order_of(order));
  case 4:
    return atomic_fetch_sub_explicit(&a->cell.b32, (uint32_t)value, order_of(order));
  default:
    return atomic_fetch_sub_explicit(&a->cell.b64, value, order_of(order));
  }
}

uint64_t zyxrt_atomic_swap(void *handle, uint64_t value, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  switch (a->size) {
  case 1:
    return atomic_exchange_explicit(&a->cell.b8, (uint8_t)value, order_of(order));
  case 2:
    return atomic_exchange_explicit(&a->cell.b16, (uint16_t)value, order_of(order));
  case 4:
    return atomic_exchange_explicit(&a->cell.b32, (uint32_t)value, order_of(order));
  default:
    return atomic_exchange_explicit(&a->cell.b64, value, order_of(order));
  }
}

int zyxrt_atomic_cas(void *handle, uint64_t expect, uint64_t value, int64_t order) {
  zyxrt_atomic *a = (zyxrt_atomic *)handle;
  memory_order o = order_of(order);
  switch (a->size) {
  case 1: {
    uint8_t e = (uint8_t)expect;
    return atomic_compare_exchange_strong_explicit(&a->cell.b8, &e, (uint8_t)value, o, o);
  }
  case 2: {
    uint16_t e = (uint16_t)expect;
    return atomic_compare_exchange_strong_explicit(&a->cell.b16, &e, (uint16_t)value, o, o);
  }
  case 4: {
    uint32_t e = (uint32_t)expect;
    return atomic_compare_exchange_strong_explicit(&a->cell.b32, &e, (uint32_t)value, o, o);
  }
  default: {
    uint64_t e = expect;
    return atomic_compare_exchange_strong_explicit(&a->cell.b64, &e, value, o, o);
  }
  }
}

void zyxrt_atomic_fence(int64_t order) {
  atomic_thread_fence(order_of(order));
}

/* A wait group counts the coroutines a waiter waits for. Its lock guards the
   count and the list together: a waiter that looked at the count and then
   listed itself must not miss a wake that fell between the two. */
typedef struct zyxrt_wg {
  int64_t count;
  zyxrt_waitlist waiters;
  zyxrt_lock lock;
} zyxrt_wg;

void *zyxrt_wg_new(void) {
  zyxrt_wg *wg = (zyxrt_wg *)zyxrt_region_alloc_current(sizeof(zyxrt_wg), 8);
  wg->count = 0;
  wg->waiters.head = NULL;
  zyxrt_lock_init(&wg->lock);
  return wg;
}

void zyxrt_wg_add(void *handle, int64_t n) {
  zyxrt_wg *wg = (zyxrt_wg *)handle;
  zyxrt_lock_hold(&wg->lock);
  wg->count += n;
  /* A group that has reached zero lets its waiters go; a group that is
     counted up again simply waits for the next time. */
  if (wg->count <= 0) zyxrt_wait_wake(&wg->waiters);
  zyxrt_lock_drop(&wg->lock);
}

void zyxrt_wg_wait(void *handle) {
  zyxrt_wg *wg = (zyxrt_wg *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  zyxrt_lock_hold(&wg->lock);
  while (wg->count > 0) {
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&wg->waiters, &w);
    zyxrt_lock_drop(&wg->lock);
    zyxrt_sched_suspend(self);
    zyxrt_lock_hold(&wg->lock);
  }
  zyxrt_lock_drop(&wg->lock);
}

/* A once runs one function exactly once. The first caller runs it and the
   others wait for it to finish, which is what makes the value the function
   builds visible to every caller. */
typedef struct zyxrt_once {
  int done;
  zyxrt_waitlist waiters;
  zyxrt_lock lock;
} zyxrt_once;

void *zyxrt_once_new(void) {
  zyxrt_once *o = (zyxrt_once *)zyxrt_region_alloc_current(sizeof(zyxrt_once), 8);
  o->done = 0;
  o->waiters.head = NULL;
  zyxrt_lock_init(&o->lock);
  return o;
}

int zyxrt_once_enter(void *handle) {
  zyxrt_once *o = (zyxrt_once *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  zyxrt_lock_hold(&o->lock);
  if (self) {
    /* Another caller is running the function, so wait for it to finish: what
       it builds must be visible before anyone returns. */
    while (o->done == 1) {
      zyxrt_waiter w = {self, NULL, 0};
      zyxrt_wait_add(&o->waiters, &w);
      zyxrt_lock_drop(&o->lock);
      zyxrt_sched_suspend(self);
      zyxrt_lock_hold(&o->lock);
    }
  }
  if (o->done == 2) {
    zyxrt_lock_drop(&o->lock);
    return 0;
  }
  /* The caller that gets here runs the function. */
  o->done = 1;
  zyxrt_lock_drop(&o->lock);
  return 1;
}

void zyxrt_once_leave(void *handle) {
  zyxrt_once *o = (zyxrt_once *)handle;
  zyxrt_lock_hold(&o->lock);
  o->done = 2;
  zyxrt_wait_wake(&o->waiters);
  zyxrt_lock_drop(&o->lock);
}

/* A mutex is held by one coroutine at a time. Waiting parks the coroutine,
   so a lock that is contended costs no scheduler thread, and a monitor
   method releases the lock while it waits, as a condition variable does. */
typedef struct zyxrt_mutex {
  int locked;
  zyxrt_waitlist waiters;
  zyxrt_waitlist monitors;
  /* The lock guards the flag and both lists; it is the object's own mutex
     rather than the one the language's Mutex exposes, which is this flag. */
  zyxrt_lock lock;
} zyxrt_mutex;

void *zyxrt_mutex_new(void) {
  zyxrt_mutex *m = (zyxrt_mutex *)zyxrt_region_alloc_current(sizeof(zyxrt_mutex), 8);
  m->locked = 0;
  m->waiters.head = NULL;
  m->monitors.head = NULL;
  zyxrt_lock_init(&m->lock);
  return m;
}

void zyxrt_mutex_lock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  zyxrt_lock_hold(&m->lock);
  while (m->locked) {
    if (!self) {
      zyxrt_lock_drop(&m->lock);
      return;
    }
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&m->waiters, &w);
    zyxrt_lock_drop(&m->lock);
    zyxrt_sched_suspend(self);
    zyxrt_lock_hold(&m->lock);
  }
  m->locked = 1;
  zyxrt_lock_drop(&m->lock);
}

int zyxrt_mutex_try_lock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_lock_hold(&m->lock);
  int taken = 0;
  if (!m->locked) {
    m->locked = 1;
    taken = 1;
  }
  zyxrt_lock_drop(&m->lock);
  return taken;
}

void zyxrt_mutex_unlock(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_lock_hold(&m->lock);
  m->locked = 0;
  if (m->waiters.head) zyxrt_wait_wake(&m->waiters);
  zyxrt_lock_drop(&m->lock);
}

/* zyxrt_mutex_wait releases the lock and parks the coroutine until a monitor
   notification arrives, then takes the lock again. The caller must hold the
   lock. */
void zyxrt_mutex_wait(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  zyxrt_lock_hold(&m->lock);
  m->locked = 0;
  if (m->waiters.head) zyxrt_wait_wake(&m->waiters);
  if (self) {
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&m->monitors, &w);
    zyxrt_lock_drop(&m->lock);
    zyxrt_sched_suspend(self);
  } else {
    zyxrt_lock_drop(&m->lock);
  }
  zyxrt_mutex_lock(handle);
}

void zyxrt_mutex_notify_one(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_lock_hold(&m->lock);
  zyxrt_waiter *w = m->monitors.head;
  if (w != NULL) {
    m->monitors.head = w->next;
    zyxrt_sched_ready(w->coro);
  }
  zyxrt_lock_drop(&m->lock);
}

void zyxrt_mutex_notify_all(void *handle) {
  zyxrt_mutex *m = (zyxrt_mutex *)handle;
  zyxrt_lock_hold(&m->lock);
  zyxrt_wait_wake(&m->monitors);
  zyxrt_lock_drop(&m->lock);
}

/* A semaphore holds a count of permits. */
typedef struct zyxrt_sem {
  int64_t count;
  zyxrt_waitlist waiters;
  zyxrt_lock lock;
} zyxrt_sem;

void *zyxrt_sem_new(int64_t n) {
  zyxrt_sem *s = (zyxrt_sem *)zyxrt_region_alloc_current(sizeof(zyxrt_sem), 8);
  s->count = n;
  s->waiters.head = NULL;
  zyxrt_lock_init(&s->lock);
  return s;
}

void zyxrt_sem_acquire(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self) return;
  zyxrt_lock_hold(&s->lock);
  while (s->count <= 0) {
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&s->waiters, &w);
    zyxrt_lock_drop(&s->lock);
    zyxrt_sched_suspend(self);
    zyxrt_lock_hold(&s->lock);
  }
  s->count--;
  zyxrt_lock_drop(&s->lock);
}

int zyxrt_sem_try_acquire(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  zyxrt_lock_hold(&s->lock);
  int taken = 0;
  if (s->count > 0) {
    s->count--;
    taken = 1;
  }
  zyxrt_lock_drop(&s->lock);
  return taken;
}

void zyxrt_sem_release(void *handle) {
  zyxrt_sem *s = (zyxrt_sem *)handle;
  zyxrt_lock_hold(&s->lock);
  s->count++;
  if (s->waiters.head) zyxrt_wait_wake(&s->waiters);
  zyxrt_lock_drop(&s->lock);
}

/* Statics. A sync object a top-level binding holds lives in static storage,
   so it outlives every region and is built before the program runs. The
   slots are the program's static sync objects, and a program that asks for
   more than the pool holds is a program that asked for too many. */
#define ZYXRT_STATIC_SLOTS 1024

static zyxrt_wg static_wgs[ZYXRT_STATIC_SLOTS];
static size_t static_wg_count;

void *zyxrt_static_wg(void) {
  if (static_wg_count >= ZYXRT_STATIC_SLOTS) {
    zyxrt_panic("too many static sync objects", 29);
  }
  zyxrt_wg *wg = &static_wgs[static_wg_count++];
  wg->count = 0;
  wg->waiters.head = NULL;
  zyxrt_lock_init(&wg->lock);
  return wg;
}

static zyxrt_once static_onces[ZYXRT_STATIC_SLOTS];
static size_t static_once_count;

void *zyxrt_static_once(void) {
  if (static_once_count >= ZYXRT_STATIC_SLOTS) {
    zyxrt_panic("too many static sync objects", 29);
  }
  zyxrt_once *o = &static_onces[static_once_count++];
  o->done = 0;
  o->waiters.head = NULL;
  zyxrt_lock_init(&o->lock);
  return o;
}

static zyxrt_mutex static_mutexes[ZYXRT_STATIC_SLOTS];
static size_t static_mutex_count;

void *zyxrt_static_mutex(void) {
  if (static_mutex_count >= ZYXRT_STATIC_SLOTS) {
    zyxrt_panic("too many static sync objects", 29);
  }
  zyxrt_mutex *m = &static_mutexes[static_mutex_count++];
  m->locked = 0;
  m->waiters.head = NULL;
  m->monitors.head = NULL;
  zyxrt_lock_init(&m->lock);
  return m;
}

static zyxrt_atomic static_atomics[ZYXRT_STATIC_SLOTS];
static size_t static_atomic_count;

void *zyxrt_static_atomic(int64_t size, uint64_t value) {
  if (static_atomic_count >= ZYXRT_STATIC_SLOTS) {
    zyxrt_panic("too many static sync objects", 29);
  }
  zyxrt_atomic *a = &static_atomics[static_atomic_count++];
  a->size = (size_t)size;
  switch (a->size) {
  case 1:
    atomic_init(&a->cell.b8, (uint8_t)value);
    break;
  case 2:
    atomic_init(&a->cell.b16, (uint16_t)value);
    break;
  case 4:
    atomic_init(&a->cell.b32, (uint32_t)value);
    break;
  default:
    atomic_init(&a->cell.b64, value);
    break;
  }
  return a;
}
