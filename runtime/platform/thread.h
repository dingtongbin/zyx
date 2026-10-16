/* Thread, mutex, and condition primitives. The runtime starts threads only
   for the blocking pool and the IO poller; the scheduler itself runs on the
   thread that started the program, and it is the only thread that ever
   readies a coroutine. */

#ifndef ZYXRUNTIME_THREAD_H
#define ZYXRUNTIME_THREAD_H

#include "zyxrt_internal.h"

#if defined(__wasm__)

/* A wasm target has no threads, so the runtime starts none and the
   primitives are the single-threaded no-ops the rest of the runtime
   compiles against. */
typedef int zyxrt_lock;
typedef int zyxrt_cond;

static inline void zyxrt_lock_init(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { (void)l; }
static inline void zyxrt_cond_init(zyxrt_cond *c) { (void)c; }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) { (void)c; (void)l; }
static inline void zyxrt_cond_signal(zyxrt_cond *c) { (void)c; }
static inline void zyxrt_thread_start(void (*run)(void *), void *arg) { (void)run; (void)arg; }

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 0; }

#elif defined(_WIN32)

#include <windows.h>

typedef CRITICAL_SECTION zyxrt_lock;
typedef CONDITION_VARIABLE zyxrt_cond;

static inline void zyxrt_lock_init(zyxrt_lock *l) { InitializeCriticalSection(l); }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { EnterCriticalSection(l); }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { LeaveCriticalSection(l); }
static inline void zyxrt_cond_init(zyxrt_cond *c) { InitializeConditionVariable(c); }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) {
  SleepConditionVariableCS(c, l, INFINITE);
}
static inline void zyxrt_cond_signal(zyxrt_cond *c) { WakeConditionVariable(c); }
static inline void zyxrt_thread_start(void (*run)(void *), void *arg) {
  CreateThread(NULL, 0, (LPTHREAD_START_ROUTINE)run, arg, 0, NULL);
}

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 1; }

#else

#include <pthread.h>

typedef pthread_mutex_t zyxrt_lock;
typedef pthread_cond_t zyxrt_cond;

static inline void zyxrt_lock_init(zyxrt_lock *l) { pthread_mutex_init(l, NULL); }
static inline void zyxrt_lock_hold(zyxrt_lock *l) { pthread_mutex_lock(l); }
static inline void zyxrt_lock_drop(zyxrt_lock *l) { pthread_mutex_unlock(l); }
static inline void zyxrt_cond_init(zyxrt_cond *c) { pthread_cond_init(c, NULL); }
static inline void zyxrt_cond_wait(zyxrt_cond *c, zyxrt_lock *l) { pthread_cond_wait(c, l); }
static inline void zyxrt_cond_signal(zyxrt_cond *c) { pthread_cond_signal(c); }

static inline void zyxrt_thread_start(void (*run)(void *), void *arg) {
  pthread_t thread;
  pthread_create(&thread, NULL, (void *(*)(void *))run, arg);
}

/* zyxrt_threads answers whether the target runs threads, which decides
   whether the event loop has a poller of its own. */
static inline int zyxrt_threads(void) { return 1; }

#endif

#endif /* ZYXRUNTIME_THREAD_H */
