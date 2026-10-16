/* Size-class object pools. Objects of one size class come from slabs, which
   are runs of equal-sized objects: the class of a slab is implied by the
   list it is on, so no object carries a header and returning a slab costs
   nothing per object.
 *
 * The pool caches a bounded number of free slabs per class. Beyond the cap a
 * slab goes back to the system, so the memory a peak load leaves behind is
 * the peak of live objects plus one bounded cache, and it does not move with
 * the load.
 *
 * Startup warms the pool with one slab per class, which is bounded and
 * deterministic: it makes the first allocations of a program meet no
 * allocator work, and it commits nothing beyond that. */

#include "zyxrt_internal.h"

#include <stdlib.h>

/* The sizes the pool serves, in ascending order. A larger request goes
   straight to the system. */
static const size_t class_sizes[] = {
    16,    32,    48,    64,    80,    96,    112,   128,
    160,   192,   224,   256,   320,   384,   448,   512,
    640,   768,   896,   1024,  1280,  1536,  1792,  2048,
    2560,  3072,  3584,  4096,  5120,  6144,  7168,  8192,
    10240, 12288, 14336, 16384, 20480, 24576, 28672, 32768,
};

#define ZYXXRT_CLASS_COUNT (sizeof(class_sizes) / sizeof(class_sizes[0]))

/* The objects one slab holds at least. */
#define ZYXXRT_SLAB_BYTES 4096u

/* The free slabs the pool keeps per class. */
#define ZYXXRT_SLAB_CACHE 4u

/* The free slabs, one list per class, and the lock that guards them: a
   coroutine may run on any scheduler thread, so two of them can return slabs
   at once. */
static zyxrt_slab *free_slabs[ZYXXRT_CLASS_COUNT];
static size_t free_counts[ZYXXRT_CLASS_COUNT];
static zyxrt_lock pool_lock;

/* zyxrt_pool_class returns the class that serves a size, or the class count
   when no class does. */
size_t zyxrt_pool_class(size_t size) {
  for (size_t i = 0; i < ZYXXRT_CLASS_COUNT; i++) {
    if (size <= class_sizes[i]) {
      return i;
    }
  }
  return ZYXXRT_CLASS_COUNT;
}

/* zyxrt_pool_class_size returns the object size of a class. */
size_t zyxrt_pool_class_size(size_t class) { return class_sizes[class]; }

/* zyxrt_pool_classes returns how many classes the pool serves. */
size_t zyxrt_pool_classes(void) { return ZYXXRT_CLASS_COUNT; }

/* make_slab returns a fresh slab of a class. */
static zyxrt_slab *make_slab(size_t class) {
  size_t object = class_sizes[class];
  size_t count = ZYXXRT_SLAB_BYTES / object;
  if (count == 0) {
    count = 1;
  }
  size_t header = (sizeof(zyxrt_slab) + 15u) / 16u * 16u;
  zyxrt_slab *slab = (zyxrt_slab *)malloc(header + count * object);
  if (slab == NULL) {
    zyxrt_panic("out of memory", 13);
  }
  slab->next = NULL;
  slab->used = 0;
  slab->count = count;
  return slab;
}

/* zyxrt_pool_take returns a slab of a class, from the cache or the system. */
zyxrt_slab *zyxrt_pool_take(size_t class) {
  zyxrt_lock_hold(&pool_lock);
  zyxrt_slab *slab = free_slabs[class];
  if (slab != NULL) {
    free_slabs[class] = slab->next;
    free_counts[class]--;
  }
  zyxrt_lock_drop(&pool_lock);
  if (slab != NULL) {
    slab->next = NULL;
    slab->used = 0;
    return slab;
  }
  return make_slab(class);
}

/* zyxrt_pool_release returns a slab to its class, or to the system when the
   cache for that class is full. */
void zyxrt_pool_release(size_t class, zyxrt_slab *slab) {
  zyxrt_lock_hold(&pool_lock);
  if (free_counts[class] < ZYXXRT_SLAB_CACHE) {
    slab->next = free_slabs[class];
    free_slabs[class] = slab;
    free_counts[class]++;
    zyxrt_lock_drop(&pool_lock);
    return;
  }
  zyxrt_lock_drop(&pool_lock);
  free(slab);
}

/* zyxrt_pool_warm fills the cache of every class with one slab, so the first
   allocations of a program meet no allocator work. */
void zyxrt_pool_warm(void) {
  zyxrt_lock_init(&pool_lock);
  for (size_t class = 0; class < ZYXXRT_CLASS_COUNT; class++) {
    if (free_slabs[class] != NULL) {
      continue;
    }
    zyxrt_slab *slab = make_slab(class);
    slab->next = free_slabs[class];
    free_slabs[class] = slab;
    free_counts[class] = 1;
  }
}
