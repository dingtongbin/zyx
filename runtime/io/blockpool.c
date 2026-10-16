/* The blocking pool. A C call that may block is handed to a thread of the
   pool, and the coroutine that made it parks until the call is done. The
   pool threads are the only part of the runtime that runs off the scheduler's
   thread, so a finished job posts its coroutine to the scheduler rather than
   readying it from the thread. */

#include "zyxrt_internal.h"
#include "thread.h"

#include <stdlib.h>

#if defined(__wasm__)

/* A wasm target has no threads, so every call runs where it stands. */
void zyxrt_block_start(void) {}
void zyxrt_block_run(zyxrt_block_fn fn, void *ctx) { fn(ctx); }

#else

/* A job is one C call waiting for a thread. */
typedef struct zyxrt_job {
  zyxrt_block_fn fn;
  void *ctx;
  zyxrt_coro *coro;
  struct zyxrt_job *next;
} zyxrt_job;

static zyxrt_lock queue_lock;
static zyxrt_cond queue_cond;
static zyxrt_job *queue_head;
static zyxrt_job *queue_tail;

/* busy counts the jobs a thread is running, and cap is the number of threads,
   which is what the runtime reports as the pool's load. */
static zyxrt_lock busy_lock;
static size_t busy;
static size_t cap;

static int started;

/* block_pool_size returns the number of pool threads, which ZYX_BLOCKPOOL
   sets and which defaults to 64. */
static size_t block_pool_size(void) {
  const char *set = getenv("ZYX_BLOCKPOOL");
  if (set != NULL && set[0] != '\0') {
    long n = strtol(set, NULL, 10);
    if (n > 0) return (size_t)n;
  }
  return 64;
}

/* worker takes jobs for as long as the process runs. */
static void worker(void *arg) {
  (void)arg;
  for (;;) {
    zyxrt_lock_hold(&queue_lock);
    while (queue_head == NULL) {
      zyxrt_cond_wait(&queue_cond, &queue_lock);
    }
    zyxrt_job *job = queue_head;
    queue_head = job->next;
    if (queue_head == NULL) queue_tail = NULL;
    zyxrt_lock_drop(&queue_lock);

    zyxrt_lock_hold(&busy_lock);
    busy++;
    zyxrt_lock_drop(&busy_lock);

    job->fn(job->ctx);

    zyxrt_coro *coro = job->coro;
    free(job);

    zyxrt_lock_hold(&busy_lock);
    busy--;
    zyxrt_lock_drop(&busy_lock);

    /* The scheduler readies the coroutine, so the thread only hands it over
       and wakes the scheduler. */
    zyxrt_sched_post(coro);
  }
}

void zyxrt_block_start(void) {
  if (started) return;
  started = 1;
  zyxrt_lock_init(&queue_lock);
  zyxrt_cond_init(&queue_cond);
  zyxrt_lock_init(&busy_lock);
  cap = block_pool_size();
  for (size_t i = 0; i < cap; i++) {
    zyxrt_thread_start(worker, NULL);
  }
}

size_t zyxrt_block_busy(void) {
  if (!started) return 0;
  zyxrt_lock_hold(&busy_lock);
  size_t n = busy;
  zyxrt_lock_drop(&busy_lock);
  return n;
}

size_t zyxrt_block_cap(void) { return cap; }

void zyxrt_block_run(zyxrt_block_fn fn, void *ctx) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (!started || self == NULL) {
    /* Without a pool, or without a coroutine to park, the call runs where it
       stands: that is what a freestanding target does. */
    fn(ctx);
    return;
  }
  zyxrt_job *job = (zyxrt_job *)malloc(sizeof(zyxrt_job));
  if (!job) zyxrt_panic("out of memory", 13);
  job->fn = fn;
  job->ctx = ctx;
  job->coro = self;
  job->next = NULL;
  zyxrt_lock_hold(&queue_lock);
  if (queue_tail != NULL) {
    queue_tail->next = job;
  } else {
    queue_head = job;
  }
  queue_tail = job;
  zyxrt_cond_signal(&queue_cond);
  zyxrt_lock_drop(&queue_lock);
  /* The coroutine waits for something the scheduler cannot see, so the main
     loop waits on the wakeup handle until the pool signals it. */
  zyxrt_sched_external(1);
  zyxrt_sched_suspend(self);
  zyxrt_sched_external(-1);
}

#endif /* __wasm__ */
