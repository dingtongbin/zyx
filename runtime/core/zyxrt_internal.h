/* Internals shared by the runtime translation units. */
#ifndef ZYXRUNTIME_INTERNAL_H
#define ZYXRUNTIME_INTERNAL_H

#include "zyxrt.h"
#include "lock.h"

#include <stdatomic.h>

/* The bits of a coroutine's scheduling word. QUEUED says a ready queue holds
   it, PARKED says no thread has it on its stack, DONE says it has finished,
   and WOKEN says a waker asked for it while neither queueing nor finishing
   was possible yet. Whoever takes the word from "a thread has it on its
   stack" to "a queue may hold it" is the one that queues it, which is what
   keeps two threads from running one stack; a coroutine that has finished is
   never queued at all, because the region that started it may release it as
   soon as the finish is published. */
#define ZYXXRT_CORO_QUEUED 1u
#define ZYXXRT_CORO_WOKEN 2u
#define ZYXXRT_CORO_PARKED 4u
#define ZYXXRT_CORO_DONE 8u

/* A coroutine no scheduler thread has run yet, and which therefore belongs to
   no thread's queue. */
#define ZYXXRT_CORO_NO_OWNER ((size_t)-1)

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

/* A timer is one coroutine's deadline. It lives on the frame of the coroutine
   that parked on it, so the wheel never allocates and a deadline costs no
   memory of its own. */
typedef struct zyxrt_timer {
  struct zyxrt_coro *coro;
  uint64_t deadline;
  struct zyxrt_timer *next;
  struct zyxrt_timer *prev;
  /* level is where the timer sits in the wheel, or -1 when it is not in it. */
  int level;
  int slot;
} zyxrt_timer;

/* The timer wheel: six levels of 64 buckets over a hundred-microsecond grain,
   which covers about eighty days. A bucket holds a chain of timers, and a
   level holds a bit per bucket so the next deadline costs a scan of the marks
   rather than of the wheel. */
#define ZYXXRT_WHEEL_LEVELS 6
#define ZYXXRT_WHEEL_SLOTS 64
#define ZYXXRT_WHEEL_SHIFT 6
#define ZYXXRT_WHEEL_GRAIN 100000ull

void zyxrt_wheel_init(void);
void zyxrt_wheel_add(zyxrt_timer *t, uint64_t deadline);
void zyxrt_wheel_remove(zyxrt_timer *t);
void zyxrt_wheel_advance(uint64_t now);
int64_t zyxrt_wheel_next(uint64_t now);

/* A coroutine. It runs on its own stack; the stack switch itself comes from
   the platform layer, which uses fibers on Windows and the ucontext
   primitives elsewhere. */
typedef struct zyxrt_coro {
  void *ctx;             /* the platform context of this coroutine */
  zyxrt_fn fn;
  void *arg;
  void *region;
  void *result;
  /* lock guards the result, the exception, the completion flag, and the list
     of coroutines waiting on this one: a coroutine may be started on one
     scheduler thread and watched from another. */
  zyxrt_lock lock;
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
  /* sched is the coroutine's scheduling word, which a waker and the thread
     running it both change: a coroutine a thread still has on its stack must
     not go on a ready queue, because another thread that took it off one would
     then run a stack this one is still using. The bits are ZYXXRT_CORO_QUEUED,
     ZYXXRT_CORO_WOKEN, and ZYXXRT_CORO_PARKED. */
  _Atomic unsigned sched;
  /* exit marks a coroutine whose body has returned. It is set on the
     coroutine's own stack and read by the scheduler thread that takes that
     stack off, which is what then publishes the finish. */
  int exit;
  /* preempt marks a coroutine the monitor has asked to let another one run,
     which it notices at the next loop back edge. */
  _Atomic int preempt;
  void *stack;
  size_t stack_size;
  /* wait_next links a coroutine into a ready queue. */
  struct zyxrt_coro *wait_next;
  /* waiters holds the waiters blocked on this one. They are woken when it
     finishes, which is what a join waits for. */
  zyxrt_waitlist waiters;
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
  /* owner is the scheduler thread the coroutine runs on, or
     ZYXXRT_CORO_NO_OWNER until one takes it. A coroutine is resumed only on
     the thread that owns it: one stack belongs to one thread, and a wake from
     another thread hands the coroutine to its owner rather than leaving it
     where any thread may take it. */
  _Atomic size_t owner;
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

/* A platform that records the stack a thread runs on has to be told which
   stack is in use, because a coroutine runs on a reservation of its own
   rather than on the thread's stack. zyxrt_ctx_bounds_thread names the
   thread's own stack and zyxrt_ctx_bounds_coro names a coroutine's; a
   platform that keeps no such record defines both as nothing. */
void zyxrt_ctx_bounds_thread(void);
void zyxrt_ctx_bounds_coro(void *stack, size_t size);

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

/* The scheduler. Coroutines run cooperatively, each on its own stack; a
   coroutine that cannot proceed suspends itself and a scheduler thread
   resumes the next ready one. The scheduler runs on as many threads as
   ZYX_THREADS names, each with a queue of its own, and a coroutine is resumed
   only on the thread that owns it. */
void zyxrt_sched_ready(zyxrt_coro *c);
/* zyxrt_sched_thread_index answers which scheduler thread the caller runs on,
   or the number of them when the caller is not one. */
size_t zyxrt_sched_thread_index(void);

/* zyxrt_sched_suspend suspends the running coroutine. */
void zyxrt_sched_suspend(zyxrt_coro *c);
/* zyxrt_sched_run is the scheduler loop of the calling thread. zyxrt_sched_init
   makes the calling thread the first of them, so a program's own thread runs
   coroutines rather than sitting outside the scheduler. */
void zyxrt_sched_init(void);
void zyxrt_sched_run(void);
void zyxrt_sched_shutdown(void);
/* zyxrt_sched_abandon ends the running coroutine and returns to the
   scheduler. A panic uses it, because the coroutine's stack must not be
   returned into. */
void zyxrt_sched_abandon(zyxrt_coro *c);
void zyxrt_sched_yield(void);
/* zyxrt_sched_sleep suspends the running coroutine until a deadline, and
   zyxrt_sched_advance readies every timer that has come due. */
void zyxrt_sched_sleep(uint64_t ns);
void zyxrt_sched_advance(void);
/* zyxrt_sched_external records that a coroutine waits for something outside
   the scheduler, such as a call handed to the blocking pool. A scheduler
   thread waits on the wakeup handle while any such wait is outstanding rather
   than deciding the program is over. */
void zyxrt_sched_external(int delta);
/* What a scheduler thread answers itself with when it has nothing to run:
   ZYXRT_WAIT_EXTERNAL means a coroutine waits outside the scheduler, and a
   negative wait other than that means no deadline is waiting either. */
#define ZYXRT_WAIT_EXTERNAL (-2)
/* The wakeup handle. A thread outside the scheduler signals it, and the
   scheduler waits on it. zyxrt_wakeup_signal releases one waiting thread and
   zyxrt_wakeup_signal_all releases count of them. */
void zyxrt_wakeup_init(void);
void zyxrt_wakeup_signal(void);
void zyxrt_wakeup_signal_all(size_t count);
/* zyxrt_wakeup_wait waits for the handle, up to ns nanoseconds, or forever
   when ns is negative. */
void zyxrt_wakeup_wait(int64_t ns);

/* Stopping the threads the runtime started. The program's own thread ends the
   process, so each of these tells the threads of one part of the runtime to
   leave and waits until they have. */
void zyxrt_block_stop(void);
void zyxrt_loop_stop(void);
void zyxrt_preempt_stop(void);

/* A C call that may block is made on the calling thread, which is what keeps it
   cheap: the arguments are passed as they are and no coroutine moves. The
   compiler tells the runtime when a thread is inside such a call, so that a
   thread with nothing of its own to run takes over the rest of the program
   while the call is in it. */
void zyxrt_entersyscall(void);
void zyxrt_exitsyscall(void);

/* The event loop. A coroutine that waits on a pollable descriptor parks, and
   a poller thread watches the descriptors and hands the ready ones to the
   scheduler: the poller records what became ready, and a scheduler thread
   turns that into ready coroutines. */
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
/* zyxrt_array_set_len records that n elements are live, which a caller uses
   when it wrote them through zyxrt_array_data. */
void zyxrt_array_set_len(void *handle, size_t n);
/* zyxrt_array_append adds n elements in one step. */
void zyxrt_array_append(void *handle, const void *data, size_t n);

/* A region records the coroutines started in it, so it can wait for them
   before it releases its memory. */
void zyxrt_region_adopt(void *region, void *coro);
void zyxrt_region_detach(void *region, void *coro);
void *zyxrt_region_take_child(void *region);
void zyxrt_region_wait(void *region);
/* The clock. zyxrt_now is the monotonic time in nanoseconds, zyxrt_sleep
   waits on the scheduler's thread, and zyxrt_timer_init prepares the clock. */
void zyxrt_timer_init(void);
uint64_t zyxrt_now(void);
void zyxrt_sleep(uint64_t ns);
zyxrt_coro *zyxrt_sched_current(void);
/* The wait lists. zyxrt_wait_add puts a node at the head of a list, and
   zyxrt_wait_remove takes it off, which a waiter does with the lists it did
   not wake on. zyxrt_wait_wake readies every waiter of a list and empties
   it. */
void zyxrt_wait_add(zyxrt_waitlist *list, zyxrt_waiter *w);
void zyxrt_wait_remove(zyxrt_waitlist *list, zyxrt_waiter *w);
void zyxrt_wait_wake(zyxrt_waitlist *list);
/* zyxrt_sched_park suspends the running coroutine on the deadline, which a
   select uses together with its channel lists. */
void zyxrt_sched_park(uint64_t deadline);

/* Cooperative preemption. zyxrt_preempt_init starts the monitor thread, which
   marks every coroutine that has held its scheduler thread for a whole
   quantum; a scheduler thread is named by its index;
   zyxrt_preempt_enter and zyxrt_preempt_leave record which coroutine a thread
   is running; zyxrt_preempt_check is what a loop back edge calls. */
/* The slots the monitor watches, one per scheduler thread. The scheduler
   starts no more threads than this, because a thread the monitor cannot watch
   is a thread a coroutine can hold for as long as it likes. */
#define ZYXXRT_PREEMPT_SLOTS 256
void zyxrt_preempt_init(void);
void zyxrt_preempt_enter(zyxrt_coro *c);
void zyxrt_preempt_leave(void);
void zyxrt_preempt_check(void);

/* The stack size every coroutine starts with. */
#define ZYXXRT_STACK_MIN (2u * 1024u * 8u)

/* The stack a coroutine's reservation holds when ZYX_STACK says nothing. */
#define ZYXXRT_STACK_DEFAULT (1024u * 1024u)

/* zyxrt_stack_warm commits the pages a new coroutine is expected to use. */
void zyxrt_stack_warm(void *stack, size_t size);

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with. */
size_t zyxrt_stack_size(void);

#endif /* ZYXRUNTIME_INTERNAL_H */
