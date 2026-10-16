/* Internals shared by the runtime translation units. */
#ifndef ZYXRUNTIME_INTERNAL_H
#define ZYXRUNTIME_INTERNAL_H

#include "zyxrt.h"

/* A slab is a run of equal-sized objects. Its size class is implied by the
   list it is on, so no object carries a header. */
typedef struct zyxrt_slab {
  struct zyxrt_slab *next;
  size_t used;  /* the objects handed out of this slab */
  size_t count; /* the objects the slab holds */
} zyxrt_slab;

/* The size-class pool. A region takes slabs from it and returns them when
   it is released. */
size_t zyxrt_pool_class(size_t size);
size_t zyxrt_pool_class_size(size_t class);
size_t zyxrt_pool_classes(void);
zyxrt_slab *zyxrt_pool_take(size_t class);
void zyxrt_pool_release(size_t class, zyxrt_slab *slab);
void zyxrt_pool_warm(void);

/* A deferred call: the function to run and the arguments it was given. */
typedef struct zyxrt_defer {
  void (*call)(void *);
  void *args;
  struct zyxrt_defer *next;
} zyxrt_defer;

/* A waiter is one coroutine's place in one wait list. A coroutine has a
   single link field, which the ready queue uses, so waiting on several
   things at once, which is what a select does, takes a node per list rather
   than a link in the coroutine. A node lives on the waiter's stack while it
   waits. */
typedef struct zyxrt_waiter {
  struct zyxrt_coro *coro;
  struct zyxrt_waiter *next;
} zyxrt_waiter;

/* A wait list holds the waiters of one channel direction, one coroutine, one
   region, or the sleepers. */
typedef struct zyxrt_waitlist {
  zyxrt_waiter *head;
} zyxrt_waitlist;

/* A coroutine. It runs on its own stack; the stack switch itself comes from
   the platform layer, which uses fibers on Windows and the ucontext
   primitives elsewhere. */
typedef struct zyxrt_coro {
  void *ctx;             /* the platform context of this coroutine */
  zyxrt_fn fn;
  void *arg;
  void *region;
  void *result;
  /* defers holds the calls the coroutine has pending, newest first. */
  struct zyxrt_defer *defers;
  /* panic holds the exception the coroutine raised, or NULL. */
  void *panic;
  /* root marks the coroutine that runs the language main function. */
  int root;
  int done;
  /* queued marks a coroutine that is already on the ready queue. Two wait
     lists can wake one coroutine, because a select waits on several
     channels, and the queue holds it once. */
  int queued;
  void *stack;
  size_t stack_size;
  /* wait_next links a coroutine into the ready queue. */
  struct zyxrt_coro *wait_next;
  /* waiters holds the waiters blocked on this one. They are woken when it
     finishes, which is what a join waits for. */
  zyxrt_waitlist waiters;
  /* wake_at is the monotonic deadline a sleeping coroutine waits for, in
     nanoseconds. It is meaningful only while the coroutine is asleep. */
  uint64_t wake_at;
  /* regions holds the regions an explicit region block entered, innermost
     last. Leaving a block restores the one before it. */
  void **regions;
  size_t region_depth;
  size_t region_cap;
  /* region_next links a coroutine into the child list of the region it was
     started in, so that region can wait for it. owner_region is that
     region, which the coroutine leaves when it is released. */
  struct zyxrt_coro *region_next;
  void *owner_region;
} zyxrt_coro;

/* The platform context layer. A context runs entry on a stack of the given
   size and switches cooperatively to another context. On the architectures
   with a hand-written switch the context is one word; elsewhere it is
   whatever the platform's own facility needs. */
void *zyxrt_ctx_thread(void);
void *zyxrt_ctx_new(void *stack, size_t size);
/* zyxrt_sched_enter is where a coroutine begins. */
void zyxrt_sched_enter(void);
void zyxrt_ctx_switch(void *from, void *to);
void zyxrt_ctx_free(void *ctx);

/* The stack of a coroutine is reserved in full and committed page by page
   as it is touched, so a coroutine that uses little memory costs little
   memory and one that uses much still has room. An inaccessible page below
   the stack turns running off its end into a fault rather than into damage
   to whatever follows. */
void *zyxrt_stack_map(size_t size);
void zyxrt_stack_unmap(void *stack, size_t size);

/* zyxrt_platform_init prepares the process for the runtime: the platform
   layer puts standard output in the mode the runtime writes in. */
void zyxrt_platform_init(void);

/* The scheduler. Coroutines run cooperatively on the calling thread: a
   coroutine that cannot proceed suspends itself and the scheduler resumes
   the next ready one. */
void zyxrt_sched_init(void);
void zyxrt_sched_shutdown(void);
void zyxrt_sched_ready(zyxrt_coro *c);
void zyxrt_sched_suspend(zyxrt_coro *c);
/* zyxrt_sched_abandon ends the running coroutine and returns to the
   scheduler. A panic uses it, because the coroutine's stack must not be
   returned into. */
void zyxrt_sched_abandon(zyxrt_coro *c);
void zyxrt_sched_yield(void);
/* zyxrt_sched_sleep suspends the running coroutine until a deadline, and
   zyxrt_sched_wait readies every sleeper whose deadline has passed and
   reports how long the scheduler should wait for the next one, or -1 when
   none is asleep. */
void zyxrt_sched_sleep(uint64_t ns);
/* The buffer of an array, which the runtime writes into when it fills an
   array in one step. */
unsigned char *zyxrt_array_data(void *handle);
/* zyxrt_array_append adds n elements in one step. */
void zyxrt_array_append(void *handle, const void *data, size_t n);

/* A region records the coroutines started in it, so it can wait for them
   before it releases its memory. */
void zyxrt_region_adopt(void *region, void *coro);
void zyxrt_region_detach(void *region, void *coro);
void *zyxrt_region_take_child(void *region);
void zyxrt_region_wait(void *region);
int64_t zyxrt_sched_wait(void);
/* The clock. zyxrt_now is the monotonic time in nanoseconds, zyxrt_sleep
   waits on the scheduler's thread, and zyxrt_timer_init prepares the clock. */
void zyxrt_timer_init(void);
uint64_t zyxrt_now(void);
void zyxrt_sleep(uint64_t ns);
int zyxrt_sched_run_once(void);
zyxrt_coro *zyxrt_sched_current(void);
/* The wait lists. zyxrt_wait_add puts a node at the head of a list, and
   zyxrt_wait_remove takes it off, which a waiter does with the lists it did
   not wake on. zyxrt_wait_wake readies every waiter of a list and empties
   it. */
void zyxrt_wait_add(zyxrt_waitlist *list, zyxrt_waiter *w);
void zyxrt_wait_remove(zyxrt_waitlist *list, zyxrt_waiter *w);
void zyxrt_wait_wake(zyxrt_waitlist *list);
/* zyxrt_sched_sleepers is the list of deadlines, which a select puts its own
   node on so the node lives on its frame. */
zyxrt_waitlist *zyxrt_sched_sleepers(void);
/* zyxrt_sched_park suspends the running coroutine on the deadline, which a
   select uses together with its channel lists. */
void zyxrt_sched_park(uint64_t deadline);

/* The stack size every coroutine starts with. */
#define ZYXXRT_STACK_MIN (2u * 1024u * 8u)

/* The stack a coroutine's reservation holds when ZYX_STACK says nothing. */
#define ZYXXRT_STACK_DEFAULT (1024u * 1024u)

/* zyxrt_stack_warm commits the pages a new coroutine is expected to use. */
void zyxrt_stack_warm(void *stack, size_t size);

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with. */
size_t zyxrt_stack_size(void);

#endif /* ZYXRUNTIME_INTERNAL_H */
