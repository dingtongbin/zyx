/* The timer wheel. Deadlines are the largest source of events a server has,
   so the structure that holds them must not scan them: a timer goes into a
   bucket of a level that covers its distance, and the clock walks the buckets
   rather than the timers. Insertion and removal are constant time, and the
   work an advance does is bounded by the timers that came due.

   Six levels of 64 buckets, each level's bucket a hundred microseconds times
   64 to the power of the level: the first level covers 6.4 milliseconds at a
   hundred microseconds of granularity, and the whole wheel covers about
   eighty days. A timer further out than that waits in the last bucket and is
   placed again each time the wheel comes round to it. */

#include "zyxrt_internal.h"

/* The buckets, one chain per bucket. A bucket's chain is doubly linked, so a
   timer a coroutine stopped waiting for leaves it in constant time. */
static zyxrt_timer *buckets[ZYXXRT_WHEEL_LEVELS][ZYXXRT_WHEEL_SLOTS];

/* nonempty marks the buckets that hold a timer, one bit per slot, so finding
   the next deadline costs a scan of the marks rather than of the wheel. */
static uint64_t nonempty[ZYXXRT_WHEEL_LEVELS];

/* base is where the level's current rotation starts: bucket i of level L
   covers [base[L] + i * span(L), base[L] + (i + 1) * span(L)). */
static uint64_t base[ZYXXRT_WHEEL_LEVELS];

/* timers counts the deadlines the wheel holds, so a thread that has none can
   tell without taking the lock and without reading the machine clock. */
static _Atomic size_t timers;

/* The wheel belongs to the whole runtime rather than to one scheduler thread:
   a coroutine may park or be woken on any of them, so every entry point takes
   this lock. */
static zyxrt_lock wheel_lock;

/* span returns the nanoseconds one bucket of a level covers. */
static uint64_t span(int level) {
  return ZYXXRT_WHEEL_GRAIN << (ZYXXRT_WHEEL_SHIFT * level);
}

void zyxrt_wheel_init(void) {
  zyxrt_lock_init(&wheel_lock);
  zyxrt_lock_hold(&wheel_lock);
  for (int level = 0; level < ZYXXRT_WHEEL_LEVELS; level++) {
    for (int slot = 0; slot < ZYXXRT_WHEEL_SLOTS; slot++) {
      buckets[level][slot] = NULL;
    }
    nonempty[level] = 0;
    /* A level starts at the bucket the clock is in, so an advance walks
       forward from now rather than from the beginning of time. */
    base[level] = zyxrt_now() / span(level) * span(level);
  }
  zyxrt_lock_drop(&wheel_lock);
}

/* link puts a timer in a bucket. */
static void link(zyxrt_timer *t, int level, int slot) {
  t->level = level;
  t->slot = slot;
  t->prev = NULL;
  t->next = buckets[level][slot];
  if (t->next != NULL) {
    t->next->prev = t;
  }
  buckets[level][slot] = t;
  nonempty[level] |= (uint64_t)1 << slot;
}

/* unlink takes a timer out of its bucket. */
static void unlink(zyxrt_timer *t) {
  if (t->level < 0) {
    return;
  }
  int level = t->level;
  int slot = t->slot;
  if (t->prev != NULL) {
    t->prev->next = t->next;
  } else {
    buckets[level][slot] = t->next;
  }
  if (t->next != NULL) {
    t->next->prev = t->prev;
  }
  t->next = NULL;
  t->prev = NULL;
  t->level = -1;
  if (buckets[level][slot] == NULL) {
    nonempty[level] &= ~((uint64_t)1 << slot);
  }
}

/* place puts a timer at the lowest level whose span holds its deadline. The
   bucket is never one the clock has already passed, because a bucket the
   clock passed is where a timer would wait forever. */
static void place(zyxrt_timer *t) {
  uint64_t now = zyxrt_now();
  uint64_t delta = t->deadline > now ? t->deadline - now : 0;
  int level = 0;
  while (level + 1 < ZYXXRT_WHEEL_LEVELS && delta >= span(level) * ZYXXRT_WHEEL_SLOTS) {
    level++;
  }
  uint64_t size = span(level);
  uint64_t current = base[level] / size;
  uint64_t index = t->deadline / size;
  if (index < current) {
    index = current;
  }
  if (index >= current + ZYXXRT_WHEEL_SLOTS) {
    /* Further out than the level covers while the clock is where it is: the
       last bucket holds it, and placing it again brings it nearer. */
    index = current + ZYXXRT_WHEEL_SLOTS - 1;
  }
  link(t, level, (int)(index % ZYXXRT_WHEEL_SLOTS));
}

void zyxrt_wheel_add(zyxrt_timer *t, uint64_t deadline) {
  zyxrt_lock_hold(&wheel_lock);
  t->deadline = deadline;
  t->next = NULL;
  t->prev = NULL;
  t->level = -1;
  place(t);
  atomic_fetch_add_explicit(&timers, 1, memory_order_relaxed);
  zyxrt_lock_drop(&wheel_lock);
  /* A scheduler thread that found nothing to run waits on the wakeup handle
     with the distance to the next deadline, or forever when the wheel was
     empty. A new deadline is a reason to look again, so the waiters are
     woken rather than left waiting for a deadline none of them knew of. */
  zyxrt_sched_wake();
}

void zyxrt_wheel_remove(zyxrt_timer *t) {
  zyxrt_lock_hold(&wheel_lock);
  if (t->level >= 0) atomic_fetch_sub_explicit(&timers, 1, memory_order_relaxed);
  unlink(t);
  zyxrt_lock_drop(&wheel_lock);
}

/* zyxrt_wheel_pending answers how many deadlines the wheel holds. */
size_t zyxrt_wheel_pending(void) { return atomic_load_explicit(&timers, memory_order_relaxed); }

/* advance moves the clock forward. Every bucket the clock has passed is
   emptied: a timer that is due is readied, and one whose deadline the bucket
   only partly covered is placed again, which is what keeps a timer from
   firing early. The upper levels are emptied first, so a timer they hold is
   placed in a level the walk below has not reached yet. */
void zyxrt_wheel_advance(uint64_t now) {
  zyxrt_lock_hold(&wheel_lock);
  for (int level = ZYXXRT_WHEEL_LEVELS - 1; level >= 0; level--) {
    uint64_t size = span(level);
    for (;;) {
      uint64_t index = base[level] / size;
      uint64_t start = index * size;
      if (start > now) {
        break;
      }
      int slot = (int)(index % ZYXXRT_WHEEL_SLOTS);
      zyxrt_timer *t = buckets[level][slot];
      buckets[level][slot] = NULL;
      nonempty[level] &= ~((uint64_t)1 << slot);
      base[level] = start + size;
      while (t != NULL) {
        zyxrt_timer *next = t->next;
        t->next = NULL;
        t->prev = NULL;
        t->level = -1;
        if (t->deadline <= now) {
          atomic_fetch_sub_explicit(&timers, 1, memory_order_relaxed);
          zyxrt_sched_ready(t->coro);
        } else {
          place(t);
        }
        t = next;
      }
    }
  }
  zyxrt_lock_drop(&wheel_lock);
}

/* zyxrt_wheel_next answers how long the scheduler may wait before something
   becomes due, or -1 when nothing waits on a deadline. It over-answers by the
   granularity of the bucket, which is why an advance places a timer whose
   deadline its bucket only partly covered. */
int64_t zyxrt_wheel_next(uint64_t now) {
  uint64_t best = 0;
  int found = 0;
  zyxrt_lock_hold(&wheel_lock);
  for (int level = 0; level < ZYXXRT_WHEEL_LEVELS; level++) {
    if (nonempty[level] == 0) {
      continue;
    }
    uint64_t size = span(level);
    uint64_t index = base[level] / size;
    uint64_t start = index * size;
    if (!found || start < best) {
      best = start;
      found = 1;
    }
  }
  zyxrt_lock_drop(&wheel_lock);
  if (!found) {
    return -1;
  }
  if (best <= now) {
    return 0;
  }
  return (int64_t)(best - now);
}
