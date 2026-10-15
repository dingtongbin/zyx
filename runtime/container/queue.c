/* PriorityQueue: a binary heap of fixed-size elements. The buffer belongs to
   the region that created the queue, so growth leaves the earlier buffers
   behind to be released with the region.

   The order comes from a comparison the caller supplies, so the same heap
   serves any element type. An element is stored as the bytes of its type and
   the comparison receives their addresses. */

#include "zyxrt.h"

#include <string.h>

#define ZYXXRT_QUEUE_MIN 8u
/* The bytes one swap moves at a time, which keeps a swap free of allocation
   whatever the element size is. */
#define ZYXXRT_SWAP_CHUNK 32u

typedef int (*zyxrt_less_fn)(const void *a, const void *b);

typedef struct zyxrt_queue {
  size_t len;
  size_t cap;
  size_t elem;
  zyxrt_less_fn less;
  unsigned char *data;
} zyxrt_queue;

/* slot returns the address of one element. */
static unsigned char *slot(zyxrt_queue *q, size_t index) {
  return q->data + index * q->elem;
}

/* swap exchanges two elements without allocating, whatever their size is. */
static void swap(unsigned char *a, unsigned char *b, size_t n) {
  unsigned char tmp[ZYXXRT_SWAP_CHUNK];
  while (n > 0) {
    size_t chunk = n < ZYXXRT_SWAP_CHUNK ? n : ZYXXRT_SWAP_CHUNK;
    memcpy(tmp, a, chunk);
    memcpy(a, b, chunk);
    memcpy(b, tmp, chunk);
    a += chunk;
    b += chunk;
    n -= chunk;
  }
}

/* grow doubles the buffer. The elements are copied and the buffer left behind
   is released with the region. */
static void grow(zyxrt_queue *q) {
  size_t cap = q->cap * 2;
  unsigned char *grown = (unsigned char *)zyxrt_region_alloc_current(cap * q->elem, 8);
  memcpy(grown, q->data, q->len * q->elem);
  q->data = grown;
  q->cap = cap;
}

void *zyxrt_queue_new(size_t elem, void *less) {
  zyxrt_queue *q = (zyxrt_queue *)zyxrt_region_alloc_current(sizeof(zyxrt_queue), 8);
  q->len = 0;
  q->cap = ZYXXRT_QUEUE_MIN;
  q->elem = elem == 0 ? 1 : elem;
  q->less = (zyxrt_less_fn)less;
  q->data = (unsigned char *)zyxrt_region_alloc_current(q->cap * q->elem, 8);
  return q;
}

size_t zyxrt_queue_len(void *handle) {
  zyxrt_queue *q = (zyxrt_queue *)handle;
  return q == NULL ? 0 : q->len;
}

/* sift_up moves the element at index towards the root while it orders before
   its parent. */
static void sift_up(zyxrt_queue *q, size_t index) {
  while (index > 0) {
    size_t parent = (index - 1) / 2;
    if (!q->less(slot(q, index), slot(q, parent))) {
      return;
    }
    swap(slot(q, index), slot(q, parent), q->elem);
    index = parent;
  }
}

/* sift_down moves the element at index towards the leaves, always towards the
   child that orders first. */
static void sift_down(zyxrt_queue *q, size_t index) {
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

void zyxrt_queue_push(void *handle, const void *value) {
  zyxrt_queue *q = (zyxrt_queue *)handle;
  if (q == NULL) return;
  if (q->len == q->cap) grow(q);
  memcpy(slot(q, q->len), value, q->elem);
  q->len++;
  sift_up(q, q->len - 1);
}

/* zyxrt_queue_pop takes the first element out. It reports whether one was
   there, so the caller can build the Option the language answers with. */
int zyxrt_queue_pop(void *handle, void *out) {
  zyxrt_queue *q = (zyxrt_queue *)handle;
  if (q == NULL || q->len == 0) return 0;
  memcpy(out, slot(q, 0), q->elem);
  q->len--;
  if (q->len > 0) {
    memcpy(slot(q, 0), slot(q, q->len), q->elem);
    sift_down(q, 0);
  }
  return 1;
}
