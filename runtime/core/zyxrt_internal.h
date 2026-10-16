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
   waits. closed marks a waiter the event loop woke because its descriptor was
   released rather than because it was ready. */
typedef struct zyxrt_waiter {
  struct zyxrt_coro *coro;
  struct zyxrt_waiter *next;
  int closed;
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
  /* watch and watch_recv are the supervisor a watched coroutine reports its
     exception to, or NULL when nothing supervises it. */
  zyxrt_watch_fn watch;
  void *watch_recv;
  /* root marks the coroutine that runs the language main function. */
  int root;
  int done;
  /* queued marks a coroutine that is already on the ready queue. Two wait
     lists can wake one coroutine, because a select waits on several
     channels, and the queue holds it once. */
  int queued;
  void *stack;
  size_t stack_size;
  /* wait_next links a coroutine into the ready queue, and post_next links it
     into the list of coroutines another thread made ready. */
  struct zyxrt_coro *wait_next;
  struct zyxrt_coro *post_next;
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
/* zyxrt_sched_external records that a coroutine waits for something outside
   the scheduler, such as a call handed to the blocking pool. The main loop
   waits on the wakeup handle while any such wait is outstanding rather than
   ending the program. */
void zyxrt_sched_external(int delta);
/* What zyxrt_sched_wait reports beyond a timeout: nothing can make progress,
   so the program ends, or a coroutine waits outside the scheduler, so the
   main loop waits on the wakeup handle. */
#define ZYXRT_WAIT_DONE (-1)
#define ZYXRT_WAIT_EXTERNAL (-2)
/* The wakeup handle. A thread outside the scheduler signals it, and the
   scheduler waits on it. */
void zyxrt_wakeup_init(void);
void zyxrt_wakeup_signal(void);
/* zyxrt_wakeup_wait waits for the handle, up to ns nanoseconds, or forever
   when ns is negative. */
void zyxrt_wakeup_wait(int64_t ns);
/* Coroutines another thread made ready. Only the scheduler thread readies a
   coroutine, so a thread that finishes work posts it and the scheduler
   drains the list; posting signals the wakeup handle, which is what tells
   the scheduler to look. */
void zyxrt_sched_post_init(void);
void zyxrt_sched_post(zyxrt_coro *c);
int zyxrt_sched_drain(void);

/* The event loop. A coroutine that waits on a pollable descriptor parks, and
   a poller thread watches the descriptors and hands the ready ones to the
   scheduler: only the scheduler thread readies a coroutine, so the poller
   records what became ready. */
void zyxrt_loop_start(void);
void zyxrt_loop_drain(void);
/* The most descriptors one backend wait reports at a time. */
#define ZYXRT_LOOP_BATCH 256
/* zyxrt_loop_snapshot fills fds and events with the registered set, which the
   targets whose kernel call takes an array of descriptors read each wait. */
size_t zyxrt_loop_snapshot(int *fds, int *events, size_t max);
/* The backend of the event loop, which each target implements over its own
   kernel facility. zyxrt_loop_backend_add sets the interest in one
   descriptor, removing it when the interest is empty. zyxrt_loop_backend_wait
   answers how many descriptors are ready, waiting up to timeout_ms
   milliseconds or forever when it is negative. */
int zyxrt_loop_backend_init(void);
int zyxrt_loop_backend_add(int fd, int events);
int zyxrt_loop_backend_wait(int *fds, int *events, int max, int timeout_ms);
void zyxrt_loop_backend_wake(void);
/* zyxrt_loop_poll_once watches the descriptors on the calling thread, which is
   what a target whose loop has no poller thread does; it answers whether
   anything became ready. A target with a poller answers no without waiting. */
int zyxrt_loop_poll_once(int64_t timeout_ns);

/* The blocking pool. zyxrt_block_run runs fn(ctx) on a pool thread and parks
   the calling coroutine until it is done. */
typedef void (*zyxrt_block_fn)(void *ctx);
void zyxrt_block_start(void);
void zyxrt_block_run(zyxrt_block_fn fn, void *ctx);
/* zyxrt_block_busy and zyxrt_block_cap report the pool's load. */
size_t zyxrt_block_busy(void);
size_t zyxrt_block_cap(void);
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
