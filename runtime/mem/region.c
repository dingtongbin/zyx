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
  /* free_objects holds the objects released before the region exits, one
     list per size class. The next free object is stored in the released
     object itself, which is why an object never needs a header. */
  void **free_objects;
  /* children holds the coroutines started in this region, which it waits
     for before it releases anything. */
  zyxrt_coro *children;
  /* lock guards the region's storage and its child list. A coroutine may run
     on any scheduler thread, and a value it creates may belong to a region
     another thread's coroutine is filling, so the region is shared. */
  zyxrt_lock lock;
  /* bootstrap marks the region the runtime made before the pools were
     ready, which allocates from the system throughout. */
  int bootstrap;
  /* pool_next links a region into the list of regions kept for reuse. */
  struct zyxrt_region *pool_next;
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

/* A region a program is done with is kept rather than freed: a program that
   enters a region block in a loop pays three allocations and three releases for
   each of them otherwise, and the arrays a region needs are the same size every
   time. The list is shared by every lane, so it is a short critical section, and
   it is bounded, so a program that entered many regions once does not keep them
   for the rest of its life. */
#define ZYXXRT_REGION_POOL 64

static zyxrt_region *pool_regions;
static size_t pool_depth;
static zyxrt_spin region_lock;

void *zyxrt_region_new(void) {
  zyxrt_region *r = NULL;
  zyxrt_spin_hold(&region_lock);
  if (pool_regions != NULL) {
    r = pool_regions;
    pool_regions = r->pool_next;
    pool_depth--;
  }
  zyxrt_spin_drop(&region_lock);
  if (r != NULL) {
    /* What the last user left in it: the chains are empty because releasing the
       region returned every slab, the runs are gone, the children have all
       finished, and the lists of released objects point at memory the region no
       longer holds. */
    r->big = NULL;
    return r;
  }
  r = (zyxrt_region *)calloc(1, sizeof(zyxrt_region));
  if (r == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  zyxrt_lock_init(&r->lock);
  r->classes = zyxrt_pool_classes();
  r->slabs = (zyxrt_slab **)calloc(r->classes, sizeof(zyxrt_slab *));
  if (r->slabs == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  r->free_objects = (void **)calloc(r->classes, sizeof(void *));
  if (r->free_objects == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  return r;
}

void zyxrt_region_free(void *region) {
  zyxrt_region *r = (zyxrt_region *)region;
  size_t class;
  if (r == NULL) {
    return;
  }
  zyxrt_lock_hold(&r->lock);
  for (class = 0; class < r->classes; class++) {
    zyxrt_slab *slab = r->slabs[class];
    r->slabs[class] = NULL;
    while (slab != NULL) {
      zyxrt_slab *next = slab->next;
      if (r->bootstrap) {
        free(slab);
      } else {
        zyxrt_pool_release(class, slab);
      }
      slab = next;
    }
  }
  {
    zyxrt_big *run = r->big;
    while (run != NULL) {
      zyxrt_big *next = run->next;
      free(run);
      run = next;
    }
  }
  r->children = NULL;
  r->big = NULL;
  if (!r->bootstrap) {
    /* The lists of released objects name memory the region no longer holds. */
    memset(r->free_objects, 0, r->classes * sizeof(void *));
  }
  zyxrt_lock_drop(&r->lock);
  /* The bootstrap region belongs to the runtime, not to a program, so it is not
     one the pool keeps. */
  if (!r->bootstrap) {
    zyxrt_spin_hold(&region_lock);
    if (pool_depth < ZYXXRT_REGION_POOL) {
      r->pool_next = pool_regions;
      pool_regions = r;
      pool_depth++;
      zyxrt_spin_drop(&region_lock);
      return;
    }
    zyxrt_spin_drop(&region_lock);
  }
  free(r->free_objects);
  free(r->slabs);
  free(r);
}

/* zyxrt_region_release returns one object to its region before the region
   exits, which is what a static release point does. The size names the class,
   so no object carries a header. A run the pool does not serve, or an object
   of the bootstrap region, is left for the region to release whole. */
void zyxrt_region_release(void *region, void *object, size_t size) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (r == NULL || object == NULL || r->bootstrap) {
    return;
  }
  size_t class = zyxrt_pool_class(size);
  if (class >= r->classes) {
    return;
  }
  zyxrt_lock_hold(&r->lock);
  *(void **)object = r->free_objects[class];
  r->free_objects[class] = object;
  zyxrt_lock_drop(&r->lock);
}

void *zyxrt_region_alloc(void *region, size_t size, size_t align) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (size == 0) size = 1;
  if (align == 0) align = 1;
  size_t class = zyxrt_pool_class(size);
  if (align > 16 || class >= r->classes || r->bootstrap) {
    zyxrt_lock_hold(&r->lock);
    void *run = alloc_big(r, size, align);
    zyxrt_lock_drop(&r->lock);
    return run;
  }
  zyxrt_lock_hold(&r->lock);
  /* An object released before the region exits is handed out again, so the
     space a program gives up is reused within the same region. */
  void *released = r->free_objects[class];
  if (released != NULL) {
    r->free_objects[class] = *(void **)released;
    zyxrt_lock_drop(&r->lock);
    return released;
  }
  zyxrt_slab *slab = r->slabs[class];
  if (slab == NULL || slab->used == slab->count) {
    zyxrt_lock_drop(&r->lock);
    slab = zyxrt_pool_take(class);
    zyxrt_lock_hold(&r->lock);
    slab->next = r->slabs[class];
    r->slabs[class] = slab;
  }
  size_t object = zyxrt_pool_class_size(class);
  void *out = object_at(slab, object, slab->used++);
  zyxrt_lock_drop(&r->lock);
  return out;
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

/* zyxrt_region_enter makes an explicit region block the one new objects
   belong to. The region the coroutine was running in is saved, so leaving
   the block restores it. */
void zyxrt_region_enter(void *region) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (!c || region == NULL) {
    return;
  }
  if (c->region_depth == c->region_cap) {
    size_t cap = c->region_cap == 0 ? 8 : c->region_cap * 2;
    void **grown = (void **)realloc(c->regions, cap * sizeof(void *));
    if (grown == NULL) {
      zyxrt_panic("out of memory", 13);
    }
    c->regions = grown;
    c->region_cap = cap;
  }
  c->regions[c->region_depth++] = c->region;
  c->region = region;
}

void zyxrt_region_leave(void) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (!c || c->region_depth == 0) {
    return;
  }
  c->region = c->regions[--c->region_depth];
}

/* zyxrt_region_adopt records that a coroutine was started in a region, so
   the region waits for it before it releases its memory. The list is doubly
   linked, because a coroutine leaves it by being released rather than by being
   taken, and a region holds one entry per coroutine it started. */
void zyxrt_region_adopt(void *region, void *coro) {
  zyxrt_region *r = (zyxrt_region *)region;
  zyxrt_coro *c = (zyxrt_coro *)coro;
  if (r == NULL || c == NULL) {
    return;
  }
  zyxrt_lock_hold(&r->lock);
  c->region_prev = NULL;
  c->region_next = r->children;
  if (r->children != NULL) {
    r->children->region_prev = c;
  }
  c->owner_region = r;
  r->children = c;
  zyxrt_lock_drop(&r->lock);
}

/* zyxrt_region_detach removes a coroutine from the region that was waiting
   for it, which is what releasing it does. A coroutine a caller joined is
   already released, so the region must not release it again. */
void zyxrt_region_detach(void *region, void *coro) {
  zyxrt_region *r = (zyxrt_region *)region;
  zyxrt_coro *c = (zyxrt_coro *)coro;
  if (r == NULL || c == NULL) {
    return;
  }
  zyxrt_lock_hold(&r->lock);
  if (c->region_prev != NULL) {
    c->region_prev->region_next = c->region_next;
  } else if (r->children == c) {
    r->children = c->region_next;
  }
  if (c->region_next != NULL) {
    c->region_next->region_prev = c->region_prev;
  }
  c->region_prev = NULL;
  c->region_next = NULL;
  c->owner_region = NULL;
  zyxrt_lock_drop(&r->lock);
}

/* zyxrt_region_take_child returns one coroutine the region still waits for,
   or NULL. */
void *zyxrt_region_take_child(void *region) {
  zyxrt_region *r = (zyxrt_region *)region;
  if (r == NULL) {
    return NULL;
  }
  zyxrt_lock_hold(&r->lock);
  zyxrt_coro *c = r->children;
  if (c != NULL) {
    r->children = c->region_next;
    if (r->children != NULL) {
      r->children->region_prev = NULL;
    }
    c->region_prev = NULL;
    c->region_next = NULL;
    c->owner_region = NULL;
  }
  zyxrt_lock_drop(&r->lock);
  return c;
}


