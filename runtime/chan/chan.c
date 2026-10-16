/* Channels. A channel carries values of one size up to a fixed capacity; a
   sender on a full channel and a receiver on an empty one suspend until the
   other side proceeds.

   A channel is shared: a sender and a receiver may be running on different
   scheduler threads, so one lock guards its ring and both its wait lists. The
   lock is released before a coroutine suspends, and the pair of "look, then
   list" under one lock is what keeps a hand-off from being lost. */

#include "zyxrt_internal.h"

#include <stdlib.h>
#include <string.h>

typedef struct zyxrt_chan {
  size_t elem;
  size_t cap;
  size_t len;
  size_t head;
  unsigned char *buf;
  int closed;
  zyxrt_waitlist senders;
  zyxrt_waitlist receivers;
  zyxrt_lock lock;
} zyxrt_chan;

void *zyxrt_chan_new(size_t elem, size_t cap) {
  if (elem == 0) elem = 1;
  if (cap == 0) cap = 1;
  zyxrt_chan *ch = (zyxrt_chan *)calloc(1, sizeof(zyxrt_chan));
  if (!ch) zyxrt_panic("out of memory", 13);
  ch->elem = elem;
  ch->cap = cap;
  ch->buf = (unsigned char *)malloc(elem * cap);
  if (!ch->buf) zyxrt_panic("out of memory", 13);
  zyxrt_lock_init(&ch->lock);
  return ch;
}

/* push appends one value to the ring. */
static void push(zyxrt_chan *ch, const void *value) {
  size_t slot = (ch->head + ch->len) % ch->cap;
  memcpy(ch->buf + slot * ch->elem, value, ch->elem);
  ch->len++;
}

/* pop removes the oldest value from the ring. */
static void pop(zyxrt_chan *ch, void *out) {
  memcpy(out, ch->buf + ch->head * ch->elem, ch->elem);
  ch->head = (ch->head + 1) % ch->cap;
  ch->len--;
}

void zyxrt_chan_send(void *handle, const void *value) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  zyxrt_coro *self = zyxrt_sched_current();
  for (;;) {
    zyxrt_lock_hold(&ch->lock);
    if (ch->closed) {
      zyxrt_lock_drop(&ch->lock);
      zyxrt_panic("send on a closed channel", 25);
    }
    if (ch->receivers.head != NULL || ch->len < ch->cap) {
      push(ch, value);
      zyxrt_wait_wake(&ch->receivers);
      zyxrt_lock_drop(&ch->lock);
      return;
    }
    /* The channel is full: list the coroutine and let another one run. */
    if (!self) {
      zyxrt_lock_drop(&ch->lock);
      zyxrt_panic("a channel blocks outside a coroutine", 36);
    }
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&ch->senders, &w);
    zyxrt_lock_drop(&ch->lock);
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_recv(void *handle, void *out) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  for (;;) {
    zyxrt_lock_hold(&ch->lock);
    if (ch->len > 0) {
      pop(ch, out);
      if (ch->senders.head != NULL) zyxrt_wait_wake(&ch->senders);
      zyxrt_lock_drop(&ch->lock);
      return 1;
    }
    if (ch->closed) {
      zyxrt_lock_drop(&ch->lock);
      return 0;
    }
    zyxrt_coro *self = zyxrt_sched_current();
    if (!self) {
      zyxrt_lock_drop(&ch->lock);
      zyxrt_panic("a channel blocks outside a coroutine", 36);
    }
    zyxrt_waiter w = {self, NULL, 0};
    zyxrt_wait_add(&ch->receivers, &w);
    zyxrt_lock_drop(&ch->lock);
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_try_send(void *handle, const void *value) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  zyxrt_lock_hold(&ch->lock);
  int sent = 0;
  if (!ch->closed) {
    if (ch->receivers.head != NULL || ch->len < ch->cap) {
      push(ch, value);
      zyxrt_wait_wake(&ch->receivers);
      sent = 1;
    }
  }
  zyxrt_lock_drop(&ch->lock);
  return sent;
}

int zyxrt_chan_try_recv(void *handle, void *out) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  zyxrt_lock_hold(&ch->lock);
  int taken = 0;
  if (ch->len > 0) {
    pop(ch, out);
    if (ch->senders.head != NULL) zyxrt_wait_wake(&ch->senders);
    taken = 1;
  }
  zyxrt_lock_drop(&ch->lock);
  return taken;
}

int zyxrt_chan_recv_ready(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  zyxrt_lock_hold(&ch->lock);
  int ready = ch->len > 0 || ch->closed;
  zyxrt_lock_drop(&ch->lock);
  return ready;
}

int zyxrt_chan_send_ready(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  zyxrt_lock_hold(&ch->lock);
  /* A send on a closed channel cannot proceed, so the arm is not ready and
     the select takes another one. A direct send panics instead, which is the
     rule for the operation rather than for the arm. */
  int ready = 0;
  if (!ch->closed) {
    ready = ch->receivers.head != NULL || ch->len < ch->cap;
  }
  zyxrt_lock_drop(&ch->lock);
  return ready;
}

void zyxrt_chan_close(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (!ch) return;
  zyxrt_lock_hold(&ch->lock);
  if (!ch->closed) {
    ch->closed = 1;
    zyxrt_wait_wake(&ch->receivers);
  }
  zyxrt_lock_drop(&ch->lock);
}

/* The nodes of one select live on its frame, which stays alive while the
   coroutine is parked. A select of up to this many arms needs no heap. */
#define ZYXRT_SELECT_INLINE 8

void zyxrt_select_wait(void *const *handles, const int *recv_arms, size_t n, uint64_t deadline) {
  zyxrt_coro *self = zyxrt_sched_current();
  if (!self || n == 0) return;
  zyxrt_waiter inline_nodes[ZYXRT_SELECT_INLINE];
  zyxrt_waiter *nodes = inline_nodes;
  if (n > ZYXRT_SELECT_INLINE) {
    nodes = (zyxrt_waiter *)calloc(n, sizeof(zyxrt_waiter));
    if (!nodes) zyxrt_panic("out of memory", 13);
  }
  size_t i;
  for (i = 0; i < n; i++) {
    zyxrt_chan *ch = (zyxrt_chan *)handles[i];
    nodes[i].coro = self;
    nodes[i].next = NULL;
    nodes[i].closed = 0;
    /* One channel at a time: the lists are not nested, so no two locks are
       held at once. */
    zyxrt_lock_hold(&ch->lock);
    zyxrt_wait_add(recv_arms[i] ? &ch->receivers : &ch->senders, &nodes[i]);
    zyxrt_lock_drop(&ch->lock);
  }
  /* A select with a timeout also waits for the deadline. */
  zyxrt_timer timer;
  int timed = deadline != 0;
  if (timed) {
    timer.coro = self;
    zyxrt_wheel_add(&timer, deadline);
  }
  zyxrt_sched_suspend(self);
  /* The lists that did not fire still hold the nodes: take them off. A wake
     empties the list it fired on, so those nodes are already gone. Each list
     is taken under its channel's lock, which is what keeps a waker on another
     thread from reading a node this frame is about to give up. */
  for (i = 0; i < n; i++) {
    zyxrt_chan *ch = (zyxrt_chan *)handles[i];
    zyxrt_lock_hold(&ch->lock);
    zyxrt_wait_remove(recv_arms[i] ? &ch->receivers : &ch->senders, &nodes[i]);
    zyxrt_lock_drop(&ch->lock);
  }
  if (timed) zyxrt_wheel_remove(&timer);
  if (nodes != inline_nodes) free(nodes);
}
