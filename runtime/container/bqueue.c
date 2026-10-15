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
     those waiting for an element. */
  zyxrt_coro *waiters_full;
  zyxrt_coro *waiters_empty;
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

/* wait puts the running coroutine on a list and suspends it. A queue that
   blocks outside a coroutine has nobody to wake it, so that is a panic. */
static void wait(zyxrt_coro **list) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (self == NULL) {
    zyxrt_panic("a queue blocks outside a coroutine", 34);
  }
  self->wait_next = *list;
  *list = self;
  zyxrt_sched_suspend(self);
}

void *zyxrt_bqueue_new(size_t elem, size_t cap, void *less) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)zyxrt_region_alloc_current(sizeof(zyxrt_bqueue), 8);
  q->len = 0;
  q->cap = cap == 0 ? 1 : cap;
  q->elem = elem == 0 ? 1 : elem;
  q->less = (zyxrt_less_fn)less;
  q->data = (unsigned char *)zyxrt_region_alloc_current(q->cap * q->elem, 8);
  q->waiters_full = NULL;
  q->waiters_empty = NULL;
  return q;
}

size_t zyxrt_bqueue_len(void *handle) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  return q == NULL ? 0 : q->len;
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
  while (q->len == q->cap) {
    wait(&q->waiters_full);
  }
  insert(q, value);
  zyxrt_sched_wake(&q->waiters_empty);
}

int zyxrt_bqueue_try_push(void *handle, const void *value) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL || q->len == q->cap) {
    return 0;
  }
  insert(q, value);
  zyxrt_sched_wake(&q->waiters_empty);
  return 1;
}

void zyxrt_bqueue_pop(void *handle, void *out) {
  zyxrt_bqueue *q = (zyxrt_bqueue *)handle;
  if (q == NULL) return;
  while (q->len == 0) {
    wait(&q->waiters_empty);
  }
  memcpy(out, slot(q, 0), q->elem);
  q->len--;
  if (q->len > 0) {
    memcpy(slot(q, 0), slot(q, q->len), q->elem);
    sift_down(q, 0);
  }
  zyxrt_sched_wake(&q->waiters_full);
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
  zyxrt_sched_wake(&q->waiters_full);
  return 1;
}
