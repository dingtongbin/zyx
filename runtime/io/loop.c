/* The event loop. A coroutine that waits on a pollable file descriptor parks,
   and a poller thread watches the descriptors and readies the coroutines that
   wait on the ones that became ready, which wakes the thread that owns them.
   The loop comes up the first time a coroutine waits on a descriptor, so a
   program that waits on none pays for neither the kernel facility nor the
   thread that watches it. */

#include "zyxrt_internal.h"

#include <stdlib.h>

/* One registered descriptor. The table is indexed by the descriptor itself,
   so a lookup costs one step and the memory is proportional to the largest
   descriptor in use. */
typedef struct zyxrt_fd_entry {
  int used;              /* the descriptor is registered */
  int released;          /* the descriptor was released, so it is done with */
  int events;            /* what the poller watches for */
  zyxrt_waiter *readers; /* coroutines waiting for it to become readable */
  zyxrt_waiter *writers; /* coroutines waiting for it to become writable */
} zyxrt_fd_entry;

static zyxrt_lock loop_lock;
static zyxrt_fd_entry *table;
static size_t table_cap;
static size_t table_used;

/* loop_started says the backend answered, which happens the first time a
   coroutine waits on a descriptor: a program that waits on none pays for
   neither the backend's own wakeup pair nor the thread that watches it. */
static int loop_started;

/* quitting tells the poller threads to leave. Their handles are kept, because
   the program's own thread ends the process and must not do that while a poller
   is inside the kernel. */
static _Atomic int quitting;
static zyxrt_thread_handle *poller_handles;
static size_t pollers_live;

/* entry_of answers the entry of a descriptor, growing the table when the
   descriptor is past its end. The caller holds the loop lock. */
static zyxrt_fd_entry *entry_of(int fd) {
  if (fd < 0) return NULL;
  if ((size_t)fd >= table_cap) {
    size_t cap = table_cap == 0 ? 64 : table_cap;
    while (cap <= (size_t)fd) cap *= 2;
    zyxrt_fd_entry *grown = (zyxrt_fd_entry *)realloc(table, cap * sizeof(zyxrt_fd_entry));
    if (!grown) zyxrt_panic("out of memory", 13);
    for (size_t i = table_cap; i < cap; i++) {
      grown[i].used = 0;
      grown[i].released = 0;
      grown[i].events = 0;
      grown[i].readers = NULL;
      grown[i].writers = NULL;
    }
    table = grown;
    table_cap = cap;
  }
  return &table[fd];
}

/* wake_nodes readies every coroutine of a list of waiters. */
static void wake_nodes(zyxrt_waiter *w) {
  while (w != NULL) {
    zyxrt_waiter *next = w->next;
    zyxrt_sched_ready(w->coro);
    w = next;
  }
}

/* record turns one readiness the poller found into ready coroutines. The
   poller does it itself: a wake is safe from any thread, so there is no reason
   to hand the readiness to a scheduler thread and make it look again. */
static void ready_fd(int fd, int events);

static void record(int fd, int events) { ready_fd(fd, events); }

/* poller waits on the kernel facility for as long as the process runs, and
   every readiness it finds readies the coroutines that wait on it, which wakes
   the thread that owns them. */
static void poller(void *arg) {
  (void)arg;
  zyxrt_sched_thread_none();
  int fds[ZYXRT_LOOP_BATCH];
  int events[ZYXRT_LOOP_BATCH];
  for (;;) {
    int n = zyxrt_loop_backend_wait(fds, events, ZYXRT_LOOP_BATCH, -1);
    if (atomic_load_explicit(&quitting, memory_order_acquire)) break;
    if (n <= 0) continue;
    for (int i = 0; i < n; i++) {
      if (fds[i] < 0) continue; /* the backend's own wakeup */
      ready_fd(fds[i], events[i]);
    }
  }
}

/* zyxrt_loop_stop tells the poller threads to leave and waits until each has.
   The wake makes the kernel call return, so the thread sees the flag rather
   than waiting on a descriptor that will not become ready. */
void zyxrt_loop_stop(void) {
  if (!loop_started || poller_handles == NULL) return;
  atomic_store_explicit(&quitting, 1, memory_order_release);
  zyxrt_loop_backend_wake();
  for (size_t i = 0; i < pollers_live; i++) {
    zyxrt_thread_join(poller_handles[i]);
  }
  pollers_live = 0;
}

/* loop_poller_count returns the number of poller threads, which ZYX_POLLERS
   sets and which defaults to one. */
static size_t loop_poller_count(void) {
  const char *set = getenv("ZYX_POLLERS");
  if (set != NULL && set[0] != '\0') {
    long n = strtol(set, NULL, 10);
    if (n > 0) return (size_t)n;
  }
  return 1;
}

void zyxrt_loop_start(void) { zyxrt_lock_init(&loop_lock); }

/* loop_ensure starts the loop the first time something waits on a descriptor:
   the backend, which is what the kernel call belongs to, and the threads that
   drive it. */
static void loop_ensure(void) {
  zyxrt_lock_hold(&loop_lock);
  if (loop_started) {
    zyxrt_lock_drop(&loop_lock);
    return;
  }
  if (zyxrt_loop_backend_init() != 0) {
    /* A target without an event loop leaves every wait to the blocking
       pool. */
    zyxrt_lock_drop(&loop_lock);
    return;
  }
  loop_started = 1;
  /* A target whose threads are absent runs the wait on the scheduler thread. */
  if (zyxrt_threads()) {
    size_t count = loop_poller_count();
    poller_handles = (zyxrt_thread_handle *)calloc(count, sizeof(zyxrt_thread_handle));
    if (!poller_handles) zyxrt_panic("out of memory", 13);
    for (size_t i = 0; i < count; i++) {
      poller_handles[i] = zyxrt_thread_start(poller, NULL);
      pollers_live = i + 1;
    }
  }
  zyxrt_lock_drop(&loop_lock);
}

int zyxrt_loop_poll_once(int64_t timeout_ns) {
  if (zyxrt_threads()) return 0;
  loop_ensure();
  if (!loop_started) return 0;
  int timeout_ms = -1;
  if (timeout_ns >= 0) {
    timeout_ms = (int)((timeout_ns + 999999) / 1000000);
    if (timeout_ms == 0) timeout_ms = 1;
  }
  int fds[ZYXRT_LOOP_BATCH];
  int events[ZYXRT_LOOP_BATCH];
  int n = zyxrt_loop_backend_wait(fds, events, ZYXRT_LOOP_BATCH, timeout_ms);
  if (n <= 0) return 0;
  for (int i = 0; i < n; i++) {
    if (fds[i] < 0) continue;
    record(fds[i], events[i]);
  }
  return 1;
}

size_t zyxrt_loop_fds(void) {
  if (!loop_started) return 0;
  zyxrt_lock_hold(&loop_lock);
  size_t n = table_used;
  zyxrt_lock_drop(&loop_lock);
  return n;
}

size_t zyxrt_loop_snapshot(int *fds, int *events, size_t max) {
  if (!loop_started) return 0;
  size_t n = 0;
  zyxrt_lock_hold(&loop_lock);
  for (size_t fd = 0; fd < table_cap && n < max; fd++) {
    if (!table[fd].used || table[fd].events == 0) continue;
    fds[n] = (int)fd;
    events[n] = table[fd].events;
    n++;
  }
  zyxrt_lock_drop(&loop_lock);
  return n;
}

int zyxrt_loop_wait(int fd, int events) {
  loop_ensure();
  if (!loop_started) return 2;
  zyxrt_coro *self = zyxrt_sched_current();
  if (self == NULL || fd < 0 || events == 0) return 2;
  zyxrt_waiter w;
  w.coro = self;
  w.next = NULL;
  w.closed = 0;

  zyxrt_lock_hold(&loop_lock);
  zyxrt_fd_entry *e = entry_of(fd);
  if (e == NULL) {
    zyxrt_lock_drop(&loop_lock);
    return 2;
  }
  if (!e->used) {
    if (e->released) {
      /* The descriptor was released, so waiting on it again is refused
         rather than registered afresh. */
      zyxrt_lock_drop(&loop_lock);
      return 1;
    }
    e->used = 1;
    e->events = 0;
    table_used++;
  }
  if (events & ZYXRT_FD_READ) {
    w.next = e->readers;
    e->readers = &w;
  } else {
    w.next = e->writers;
    e->writers = &w;
  }
  int want = e->events | events;
  int changed = want != e->events;
  e->events = want;
  zyxrt_lock_drop(&loop_lock);

  /* The wait is counted before the kernel is asked to watch the descriptor, so
     a readiness that arrives at once cannot leave the scheduler with a
     coroutine waiting for something it cannot see and no count to say so. */
  zyxrt_sched_external(1);
  if (changed) {
    /* A descriptor the kernel does not watch yet has to be registered before
       the wait, and the poller has to look again. */
    if (zyxrt_loop_backend_add(fd, want) != 0) {
      zyxrt_lock_hold(&loop_lock);
      zyxrt_fd_entry *back = entry_of(fd);
      if (back != NULL) {
        if (back->readers == &w) back->readers = w.next;
        if (back->writers == &w) back->writers = w.next;
      }
      zyxrt_lock_drop(&loop_lock);
      zyxrt_sched_external(-1);
      return 2;
    }
    zyxrt_loop_backend_wake();
  }

  zyxrt_sched_suspend(self);
  zyxrt_sched_external(-1);

  /* Whoever readied the coroutine took the waiter off the entry first, so the
     node is free by now and only its verdict is left to read. */
  return w.closed ? 1 : 0;
}

void zyxrt_loop_release(int fd) {
  if (!loop_started || fd < 0) return;
  zyxrt_lock_hold(&loop_lock);
  zyxrt_fd_entry *e = entry_of(fd);
  if (e == NULL || !e->used) {
    zyxrt_lock_drop(&loop_lock);
    return;
  }
  e->used = 0;
  e->released = 1;
  e->events = 0;
  table_used--;
  zyxrt_waiter *readers = e->readers;
  zyxrt_waiter *writers = e->writers;
  e->readers = NULL;
  e->writers = NULL;
  /* The waiters are told the descriptor is gone rather than that it is
     ready, because there is nothing left to read or write. */
  for (zyxrt_waiter *w = readers; w != NULL; w = w->next) w->closed = 1;
  for (zyxrt_waiter *w = writers; w != NULL; w = w->next) w->closed = 1;
  zyxrt_lock_drop(&loop_lock);

  zyxrt_loop_backend_add(fd, 0);
  zyxrt_loop_backend_wake();
  wake_nodes(readers);
  wake_nodes(writers);
}

/* ready_fd turns one readiness into ready coroutines: the waiters of the
   direction that became ready run, and the interest in it is dropped until
   someone waits again. */
static void ready_fd(int fd, int events) {
  zyxrt_lock_hold(&loop_lock);
  zyxrt_fd_entry *e = entry_of(fd);
  if (e == NULL || !e->used) {
    zyxrt_lock_drop(&loop_lock);
    return;
  }
  zyxrt_waiter *readers = NULL;
  zyxrt_waiter *writers = NULL;
  if (events & ZYXRT_FD_READ) {
    readers = e->readers;
    e->readers = NULL;
    e->events &= ~ZYXRT_FD_READ;
  }
  if (events & ZYXRT_FD_WRITE) {
    writers = e->writers;
    e->writers = NULL;
    e->events &= ~ZYXRT_FD_WRITE;
  }
  int left = e->events;
  zyxrt_lock_drop(&loop_lock);

  if (readers != NULL || writers != NULL) {
    zyxrt_loop_backend_add(fd, left);
    zyxrt_loop_backend_wake();
  }
  wake_nodes(readers);
  wake_nodes(writers);
}

