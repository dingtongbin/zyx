/* Internals shared by the runtime translation units. */
#ifndef ZYXRUNTIME_INTERNAL_H
#define ZYXRUNTIME_INTERNAL_H

#include "zyxrt.h"

/* A coroutine. It runs on its own stack; the stack switch itself comes from
   the platform layer, which uses fibers on Windows and the ucontext
   primitives elsewhere. */
typedef struct zyxrt_coro {
  void *ctx;             /* the platform context of this coroutine */
  zyxrt_fn fn;
  void *arg;
  void *region;
  void *result;
  /* panic holds the exception the coroutine raised, or NULL. */
  void *panic;
  /* root marks the coroutine that runs the language main function. */
  int root;
  int done;
  void *stack;
  size_t stack_size;
  /* wait_next links a coroutine into a wait list. */
  struct zyxrt_coro *wait_next;
} zyxrt_coro;

/* The platform context layer. A context runs entry on a stack of the given
   size and switches cooperatively to another context. */
void *zyxrt_ctx_thread(void);
void *zyxrt_ctx_new(void *stack, size_t size, void (*entry)(void));
void zyxrt_ctx_switch(void *from, void *to);
void zyxrt_ctx_free(void *ctx);

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
int zyxrt_sched_run_once(void);
zyxrt_coro *zyxrt_sched_current(void);
void zyxrt_sched_wake(zyxrt_coro **list);

/* The stack size every coroutine starts with. */
#define ZYXXRT_STACK_MIN (2u * 1024u * 8u)

#endif /* ZYXRUNTIME_INTERNAL_H */
