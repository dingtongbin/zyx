/* Region pools and bulk release. A region owns a chain of slabs, one chain
   per size class, plus the runs of objects too large or too aligned for the
   pool. It hands out objects by advancing through the current slab, and
   returns every slab to its class at once. */

#include "zyxrt.h"
#include "zyxrt_internal.h"

#include <stdlib.h>
#include <string.h>

/* A run of objects too large or too aligned for the pool. */
typedef struct zyxrt_big {
  struct zyxrt_big *next;
} zyxrt_big;

/* A region. */
typedef struct zyxrt_region {
  /* slabs holds the chains of pooled objects, one per size class. */
  zyxrt_slab **slabs;
  size_t classes;
  /* big holds the runs the pool does not serve. */
  zyxrt_big *big;
  /* bootstrap marks the region the runtime made before the pools were
     ready, which allocates from the system throughout. */
  int bootstrap;
} zyxrt_region;

/* The pool's class count, which the region asks for on creation. */
size_t zyxrt_pool_classes(void);

/* round_up rounds a size up to an alignment, which must be a power of two. */
static size_t round_up(size_t n, size_t a) {
  if (a <= 1) return n;
  return (n + a - 1) / a * a;
}

/* header_bytes returns the bytes a slab spends before its objects. */
static size_t header_bytes(void) { return (sizeof(zyxrt_slab) + 15u) / 16u * 16u; }

/* object_at returns the object of a given position in a slab. */
static void *object_at(zyxrt_slab *slab, size_t object, size_t index) {
  return (unsigned char *)slab + header_bytes() + index * object;
}

/* alloc_big returns a run from the system, which the region releases whole. */
static void *alloc_big(zyxrt_region *r, size_t size, size_t align) {
  size_t header = (sizeof(zyxrt_big) + align - 1) / align * align;
  zyxrt_big *run = (zyxrt_big *)malloc(header + size);
  if (run == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  run->next = r->big;
  r->big = run;
  return (unsigned char *)run + header;
}

void *zyxrt_region_new(void) {
  zyxrt_region *r = (zyxrt_region *)calloc(1, sizeof(zyxrt_region));
  if (r == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  r->classes = zyxrt_pool_classes();
  r->slabs = (zyxrt_slab **)calloc(r->classes, sizeof(zyxrt_slab *));
  if (r->slabs == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  return r;
}

void zyxrt_region_free(void *region) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (r == NULL) {
    return;
  }
  if (!r->bootstrap) {
    for (size_t class = 0; class < r->classes; class++) {
      zyxrt_slab *slab = r->slabs[class];
      while (slab != NULL) {
        zyxrt_slab *next = slab->next;
        zyxrt_pool_release(class, slab);
        slab = next;
      }
    }
  } else {
    for (size_t class = 0; class < r->classes; class++) {
      zyxrt_slab *slab = r->slabs[class];
      while (slab != NULL) {
        zyxrt_slab *next = slab->next;
        free(slab);
        slab = next;
      }
    }
  }
  zyxrt_big *run = r->big;
  while (run != NULL) {
    zyxrt_big *next = run->next;
    free(run);
    run = next;
  }
  free(r->slabs);
  free(r);
}

void *zyxrt_region_alloc(void *region, size_t size, size_t align) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (size == 0) size = 1;
  if (align == 0) align = 1;
  size_t class = zyxrt_pool_class(size);
  if (align > 16 || class >= r->classes || r->bootstrap) {
    return alloc_big(r, size, align);
  }
  zyxrt_slab *slab = r->slabs[class];
  if (slab == NULL || slab->used == slab->count) {
    slab = zyxrt_pool_take(class);
    slab->next = r->slabs[class];
    r->slabs[class] = slab;
  }
  size_t object = zyxrt_pool_class_size(class);
  return object_at(slab, object, slab->used++);
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

void *zyxrt_region_alloc_current(size_t size, size_t align) {
  return zyxrt_region_alloc(zyxrt_region_current(), size, align);
}
