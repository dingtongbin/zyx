/* Region pools and bulk release. A region owns a chain of blocks, hands out
   aligned memory by advancing a pointer, and returns every block at once. */

#include "zyxrt.h"
#include "zyxrt_internal.h"

#include <stdlib.h>
#include <string.h>

/* One block of a region. */
typedef struct zyxrt_block {
  struct zyxrt_block *next;
  size_t used;
  size_t cap;
  size_t align;
  unsigned char data[];
} zyxrt_block;

/* A region. */
typedef struct zyxrt_region {
  zyxrt_block *head;
  size_t block_size;
} zyxrt_region;

#define ZYXXRT_BLOCK_MIN (64u * 1024u)

/* round_up rounds a size up to an alignment, which must be a power of two. */
static size_t round_up(size_t n, size_t a) {
  if (a <= 1) return n;
  return (n + a - 1) / a * a;
}

/* new_block allocates one block that can hold at least size bytes. */
static zyxrt_block *new_block(size_t size, size_t align) {
  size_t cap = size > ZYXXRT_BLOCK_MIN ? size : ZYXXRT_BLOCK_MIN;
  zyxrt_block *b = (zyxrt_block *)malloc(sizeof(zyxrt_block) + cap);
  if (!b) zyxrt_panic("out of memory", 13);
  b->next = NULL;
  b->used = 0;
  b->cap = cap;
  b->align = align;
  return b;
}

void *zyxrt_region_new(void) {
  zyxrt_region *r = (zyxrt_region *)malloc(sizeof(zyxrt_region));
  if (!r) zyxrt_panic("out of memory", 13);
  r->block_size = ZYXXRT_BLOCK_MIN;
  r->head = new_block(r->block_size, 16);
  return r;
}

void zyxrt_region_free(void *region) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (!r) return;
  zyxrt_block *b = r->head;
  while (b) {
    zyxrt_block *next = b->next;
    free(b);
    b = next;
  }
  free(r);
}

void *zyxrt_alloc(void *region, size_t size, size_t align) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (size == 0) size = 1;
  if (align == 0) align = 1;
  for (zyxrt_block *b = r->head; b; b = b->next) {
    size_t start = round_up(b->used, align);
    if (start + size <= b->cap) {
      void *p = b->data + start;
      b->used = start + size;
      return p;
    }
  }
  zyxrt_block *b = new_block(size, align);
  b->next = r->head;
  r->head = b;
  void *p = b->data;
  b->used = size;
  return p;
}

/* The region the bootstrap uses before the first coroutine exists. */
static void *bootstrap_region;

void *zyxrt_region_current(void) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (!c) {
    if (!bootstrap_region) bootstrap_region = zyxrt_region_new();
    return bootstrap_region;
  }
  if (!c->region) c->region = zyxrt_region_new();
  return c->region;
}

void *zyxrt_alloc_here(size_t size, size_t align) {
  return zyxrt_alloc(zyxrt_region_current(), size, align);
}
