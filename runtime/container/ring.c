/* Ring: a fixed-capacity circular buffer. The storage belongs to the region
   that created the ring, and the capacity is fixed, so a push into a full
   ring overwrites the oldest element and the ring never allocates again.

   The oldest element is at head, and the live elements run from there around
   the storage, which is what makes a push and a pop constant time. */

#include "zyxrt.h"

#include <string.h>

typedef struct zyxrt_ring {
  size_t len;
  size_t cap;
  size_t elem;
  size_t head;
  unsigned char *data;
} zyxrt_ring;

static unsigned char *slot(zyxrt_ring *r, size_t index) {
  return r->data + index * r->elem;
}

void *zyxrt_ring_new(size_t elem, size_t cap) {
  zyxrt_ring *r = (zyxrt_ring *)zyxrt_region_alloc_current(sizeof(zyxrt_ring), 8);
  r->len = 0;
  r->head = 0;
  r->cap = cap == 0 ? 1 : cap;
  r->elem = elem == 0 ? 1 : elem;
  r->data = (unsigned char *)zyxrt_region_alloc_current(r->cap * r->elem, 8);
  return r;
}

size_t zyxrt_ring_len(void *handle) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  return r == NULL ? 0 : r->len;
}

size_t zyxrt_ring_cap(void *handle) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  return r == NULL ? 0 : r->cap;
}

/* zyxrt_ring_push adds an element. A full ring drops its oldest element to
   make room, which is what a fixed ring does. */
void zyxrt_ring_push(void *handle, const void *value) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  if (r == NULL) return;
  if (r->len == r->cap) {
    memcpy(slot(r, r->head), value, r->elem);
    r->head = (r->head + 1) % r->cap;
    return;
  }
  size_t tail = (r->head + r->len) % r->cap;
  memcpy(slot(r, tail), value, r->elem);
  r->len++;
}

/* zyxrt_ring_peek copies the oldest element out without taking it, which is
   what the language's Peek answers with. */
int zyxrt_ring_peek(void *handle, void *out) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  if (r == NULL || r->len == 0) return 0;
  memcpy(out, slot(r, r->head), r->elem);
  return 1;
}

/* zyxrt_ring_clear drops every element. The capacity is fixed, so clearing
   only moves the head back to the start. */
void zyxrt_ring_clear(void *handle) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  if (r == NULL) return;
  r->len = 0;
  r->head = 0;
}

/* zyxrt_ring_pop takes the oldest element out. It reports whether one was
   there, so the caller can build the Option the language answers with. */
int zyxrt_ring_pop(void *handle, void *out) {
  zyxrt_ring *r = (zyxrt_ring *)handle;
  if (r == NULL || r->len == 0) return 0;
  memcpy(out, slot(r, r->head), r->elem);
  r->head = (r->head + 1) % r->cap;
  r->len--;
  return 1;
}
