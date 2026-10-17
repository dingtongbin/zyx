/* Locks, conditions, and threads. The runtime starts threads only for the
   scheduler, the blocking pool, the IO poller, and the preemption monitor; the
   scheduler's own threads run coroutines, and every other thread hands work to
   them. A thread the runtime starts is one it also stops: the program's own
   thread ends the process, and it must not end it while another thread is still
   inside the allocator or inside a call, so every starter keeps the handle it
   was given and waits on it. A wasm target has no threads, so the primitives
   are the single-threaded no-ops the rest of the runtime compiles against. */

#ifndef ZYXRUNTIME_LOCK_H
#define ZYXRUNTIME_LOCK_H

#include <stdatomic.h>
#include <stddef.h>

/* zyxrt_spin holds a lock over a handful of instructions, which is what a free
   list shared by the lanes needs: it is taken and released without a system
   call, and what it guards is a few pointer writes. */
typedef _Atomic int zyxrt_spin;

static inline void zyxrt_spin_init(zyxrt_spin *s) {
  atomic_store_explicit(s, 0, memory_order_relaxed);
}

static inline void zyxrt_spin_hold(zyxrt_spin *s) {
  int expected = 0;
  while (!atomic_compare_exchange_weak_explicit(s, &expected, 1,
                                                memory_order_acquire, memory_order_relaxed)) {
    expected = 0;
  }
}

static inline void zyxrt_spin_drop(zyxrt_spin *s) {
  atomic_store_explicit(s, 0, memory_order_release);
}

#if defined(__wasm__)

typedef int zyxrt_lock;
typedef int zyxrt_cond;
typedef int zyxrt_thread_handle;

static inline void zyxrt_lock_init(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_cond_init(zyxrt_cond *c) { (void)c; }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) { (void)c; (void)l; }
static inline void zyxrt_cond_signal(zyxrt_cond *c) { (void)c; }
static inline void zyxrt_cond_broadcast(zyxrt_cond *c) { (void)c; }
static inline zyxrt_thread_handle zyxrt_thread_start(void (*run)(void *), void *arg) {
  (void)run;
  (void)arg;
  return 0;
}
static inline void zyxrt_thread_join(zyxrt_thread_handle h) { (void)h; }

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 0; }

/* zyxrt_cpu_count answers how many processors can run code at once, which is
   what the scheduler sizes itself to. */
static inline size_t zyxrt_cpu_count(void) { return 1; }

#elif defined(_WIN32)

#include <windows.h>

typedef CRITICAL_SECTION zyxrt_lock;
typedef CONDITION_VARIABLE zyxrt_cond;
typedef HANDLE zyxrt_thread_handle;

static inline void zyxrt_lock_init(zyxrt_lock *l) { InitializeCriticalSection(l); }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { EnterCriticalSection(l); }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { LeaveCriticalSection(l); }
static inline void zyxrt_cond_init(zyxrt_cond *c) { InitializeConditionVariable(c); }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) {
  SleepConditionVariableCS(c, l, INFINITE);
}
static inline void zyxrt_cond_signal(zyxrt_cond *c) { WakeConditionVariable(c); }
static inline void zyxrt_cond_broadcast(zyxrt_cond *c) { WakeAllConditionVariable(c); }
static inline zyxrt_thread_handle zyxrt_thread_start(void (*run)(void *), void *arg) {
  HANDLE h = CreateThread(NULL, 0, (LPTHREAD_START_ROUTINE)run, arg, 0, NULL);
  if (h == NULL) zyxrt_panic("cannot start a thread", 25);
  return h;
}
static inline void zyxrt_thread_join(zyxrt_thread_handle h) {
  if (h == NULL) return;
  WaitForSingleObject(h, INFINITE);
  CloseHandle(h);
}

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 1; }

/* zyxrt_cpu_count answers how many processors can run code at once, which is
   what the scheduler sizes itself to. The count is the one the process may
   use, so a process confined to a subset of the machine does not start
   threads it cannot run. */
static inline size_t zyxrt_cpu_count(void) {
  SYSTEM_INFO info;
  GetSystemInfo(&info);
  return info.dwNumberOfProcessors > 0 ? (size_t)info.dwNumberOfProcessors : 1;
}

#else

#include <pthread.h>
#include <unistd.h>

typedef pthread_mutex_t zyxrt_lock;
typedef pthread_cond_t zyxrt_cond;
typedef pthread_t zyxrt_thread_handle;

static inline void zyxrt_lock_init(zyxrt_lock *l) { pthread_mutex_init(l, NULL); }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { pthread_mutex_lock(l); }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { pthread_mutex_unlock(l); }
static inline void zyxrt_cond_init(zyxrt_cond *c) { pthread_cond_init(c, NULL); }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) { pthread_cond_wait(c, l); }
static inline void zyxrt_cond_signal(zyxrt_cond *c) { pthread_cond_signal(c); }
static inline void zyxrt_cond_broadcast(zyxrt_cond *c) { pthread_cond_broadcast(c); }

static inline zyxrt_thread_handle zyxrt_thread_start(void (*run)(void *), void *arg) {
  pthread_t thread;
  /* A thread the runtime starts is one it also waits for, so it is not
     detached. Its stack is the platform's default, because a thread the
     scheduler does not switch itself may need a full one: it runs the scheduler
     loop, the poller, or the blocking call it was handed. */
  if (pthread_create(&thread, NULL, (void *(*)(void *))run, arg) != 0) {
    zyxrt_panic("cannot start a thread", 25);
  }
  return thread;
}

static inline void zyxrt_thread_join(zyxrt_thread_handle h) { pthread_join(h, NULL); }

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 1; }

/* zyxrt_cpu_count answers how many processors can run code at once, which is
   what the scheduler sizes itself to. */
static inline size_t zyxrt_cpu_count(void) {
  long count = sysconf(_SC_NPROCESSORS_ONLN);
  return count > 0 ? (size_t)count : 1;
}

#endif

#endif /* ZYXRUNTIME_LOCK_H */
