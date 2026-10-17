/* BlockingQueue: a bounded heap of fixed-size elements that coroutines share.
   A push waits while the queue is full and a pop waits while it is empty, so
   the queue is the handoff point between a producer and a consumer.

   The buffer is fixed, because the queue is bounded by construction, and it
   belongs to the region that created the queue. */

#include "zyxrt.h"
#include "zyxrt_internal.h"

#include <string.h>

#define ZYXXRT_BQUEUE_SWAP_CHUNK 32u

typedef int (*zyxrt_less_fn)(const void *a, const void *b);

typedef struct zyxrt_bqueue {
  size_t len;
  size_t cap;
  size_t elem;
  zyxrt_less_fn less;
  unsigned char *data;
  /* waiters_full holds the coroutines waiting for room, and waiters_empty
     those waiting for an element. The lock guards the heap and both lists: a
     producer and a consumer may run on different scheduler threads. */
  zyxrt_waitlist waiters_full;
  zyxrt_waitlist waiters_empty;
  zyxrt_lock lock;
} zyxrt_bqueue;

static unsigned char *slot(zyxrt_bqueue *q, size_t index) {
  return q->data + index * q->elem;
}

static void swap(unsigned char *a, unsigned char *b, size_t n) {
  unsigned char tmp[ZYXXRT_BQUEUE_SWAP_CHUNK];
  while (n > 0) {
    size_t chunk = n < ZYXXRT_BQUEUE_SWAP_CHUNK ? n : ZYXXRT_BQUEUE_SWAP_CHUNK;
    memcpy(tmp, a, chunk);
    memcpy(a, b, chunk);
    memcpy(b, tmp, chunk);
    a += chunk;
    b += chunk;
    n -= chunk;
  }
}

static void sift_up(zyxrt_bqueue *q, size_t index) {
  while (index > 0) {
    size_t parent = (index - 1) / 2;
    if (!q->less(slot(q, index), slot(q, parent))) {
      return;
    }
    swap(slot(q, index), slot(q, parent), q->elem);
    index = parent;
  }
}

static void sift_down(zyxrt_bqueue *q, size_t index) {
  for (;;) {
    size_t left = index * 2 + 1;
    size_t right = left + 1;
    size_t best = index;
    if (left < q->len && q->less(slot(q, left), slot(q, best))) {
      best = left;
    }
    if (right < q->len && q->less(slot(q, right), slot(q, best))) {
      best = right;
    }
    if (best == index) {
      return;
    }
    swap(slot(q, index), slot(q, best), q->elem);
    index = best;
  }
}

/* park lists the running coroutine on a wait list and lets another one run.
   The caller holds the queue's lock, which this releases for the suspension
   and takes again afterwards, so a wake on another thread cannot arrive
   between the look at the queue and the listing. */
static void park(zyxrt_waitlist *list, zyxrt_lock *lock) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (self == NULL) {
    zyxrt_lock_drop(lock);
    zyxrt_panic("a queue blocks outside a coroutine", 34);
  }
  zyxrt_waiter w = {self, NULL, 0};
  zyxrt_wait_add(list, &w);
  zyxrt_lock_drop(lock);
  zyxrt_sched_suspend(self);
  zyxrt_lock_hold(lock);
}

void *zyxrt_bqueue_new(size_t elem, size_t cap, void *less) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)zyxrt_region_alloc_current(sizeof(zyxrt_bqueue), 8);
  q->len = 0;
  q->cap = cap == 0 ? 1 : cap;
  q->elem = elem == 0 ? 1 : elem;
  q->less = (zyxrt_less_fn)less;
  q->data = (unsigned char *)zyxrt_region_alloc_current(q->cap * q->elem, 8);
  q->waiters_full.head = NULL;
  q->waiters_empty.head = NULL;
  zyxrt_lock_init(&q->lock);
  return q;
}

size_t zyxrt_bqueue_len(void *handle) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return 0;
  zyxrt_lock_hold(&q->lock);
  size_t n = q->len;
  zyxrt_lock_drop(&q->lock);
  return n;
}

size_t zyxrt_bqueue_cap(void *handle) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return 0;
  zyxrt_lock_hold(&q->lock);
  size_t cap = q->cap;
  zyxrt_lock_drop(&q->lock);
  return cap;
}

/* insert places an element and restores the heap order. */
static void insert(zyxrt_bqueue *q, const void *value) {
  memcpy(slot(q, q->len), value, q->elem);
  q->len++;
  sift_up(q, q->len - 1);
}

void zyxrt_bqueue_push(void *handle, const void *value) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return;
  zyxrt_lock_hold(&q->lock);
  while (q->len == q->cap) {
    park(&q->waiters_full, &q->lock);
  }
  insert(q, value);
  zyxrt_wait_wake(&q->waiters_empty);
  zyxrt_lock_drop(&q->lock);
}

int zyxrt_bqueue_try_push(void *handle, const void *value) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return 0;
  zyxrt_lock_hold(&q->lock);
  int pushed = 0;
  if (q->len < q->cap) {
    insert(q, value);
    zyxrt_wait_wake(&q->waiters_empty);
    pushed = 1;
  }
  zyxrt_lock_drop(&q->lock);
  return pushed;
}

void zyxrt_bqueue_pop(void *handle, void *out) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return;
  zyxrt_lock_hold(&q->lock);
  while (q->len == 0) {
    park(&q->waiters_empty, &q->lock);
  }
  memcpy(out, slot(q, 0), q->elem);
  q->len--;
  if (q->len > 0) {
    memcpy(slot(q, 0), slot(q, q->len), q->elem);
    sift_down(q, 0);
  }
  zyxrt_wait_wake(&q->waiters_full);
  zyxrt_lock_drop(&q->lock);
}

int zyxrt_bqueue_try_pop(void *handle, void *out) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL || q->len == 0) {
    return 0;
  }
  memcpy(out, slot(q, 0), q->elem);
  q->len--;
  if (q->len > 0) {
    memcpy(slot(q, 0), slot(q, q->len), q->elem);
    sift_down(q, 0);
  }
  zyxrt_wait_wake(&q->waiters_full);
  return 1;
}
