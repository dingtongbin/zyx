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
  void *stack;
  size_t stack_size;
  /* wait_next links a coroutine into a wait list. */
  struct zyxrt_coro *wait_next;
  /* waiters holds the coroutines blocked on this one, newest first. They are
     woken when it finishes, which is what a join waits for. */
  struct zyxrt_coro *waiters;
  /* wake_at is the monotonic deadline a sleeping coroutine waits for, in
     nanoseconds. It is meaningful only while the coroutine is asleep. */
  uint64_t wake_at;
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
int64_t zyxrt_sched_wait(void);
/* The clock. zyxrt_now is the monotonic time in nanoseconds, zyxrt_sleep
   waits on the scheduler's thread, and zyxrt_timer_init prepares the clock. */
void zyxrt_timer_init(void);
uint64_t zyxrt_now(void);
void zyxrt_sleep(uint64_t ns);
int zyxrt_sched_run_once(void);
zyxrt_coro *zyxrt_sched_current(void);
void zyxrt_sched_wake(zyxrt_coro **list);

/* The stack size every coroutine starts with. */
#define ZYXXRT_STACK_MIN (2u * 1024u * 8u)

/* The stack a coroutine's reservation holds when ZYX_STACK says nothing. */
#define ZYXXRT_STACK_DEFAULT (1024u * 1024u)

/* zyxrt_stack_warm commits the pages a new coroutine is expected to use. */
void zyxrt_stack_warm(void *stack, size_t size);

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with. */
size_t zyxrt_stack_size(void);

#endif /* ZYXRUNTIME_INTERNAL_H */
