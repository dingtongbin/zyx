/* Channels. A channel carries values of one size up to a fixed capacity; a
   sender on a full channel and a receiver on an empty one suspend until the
   other side proceeds. */

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
  for (;;) {
    if (ch->closed) zyxrt_panic("send on a closed channel", 25);
    if (ch->receivers.head) {
      push(ch, value);
      zyxrt_wait_wake(&ch->receivers);
      return;
    }
    if (ch->len < ch->cap) {
      push(ch, value);
      return;
    }
    /* The channel is full: suspend until a receiver drains it. */
    zyxrt_coro *self = zyxrt_sched_current();
    if (!self) zyxrt_panic("a channel blocks outside a coroutine", 36);
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&ch->senders, &w);
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_recv(void *handle, void *out) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  for (;;) {
    if (ch->len > 0) {
      pop(ch, out);
      if (ch->senders.head) zyxrt_wait_wake(&ch->senders);
      return 1;
    }
    if (ch->closed) return 0;
    zyxrt_coro *self = zyxrt_sched_current();
    if (!self) zyxrt_panic("a channel blocks outside a coroutine", 36);
    zyxrt_waiter w = {self, NULL};
    zyxrt_wait_add(&ch->receivers, &w);
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_try_send(void *handle, const void *value) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (ch->closed) return 0;
  if (ch->receivers.head) {
    push(ch, value);
    zyxrt_wait_wake(&ch->receivers);
    return 1;
  }
  if (ch->len >= ch->cap) return 0;
  push(ch, value);
  return 1;
}

int zyxrt_chan_try_recv(void *handle, void *out) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (ch->len == 0) return 0;
  pop(ch, out);
  if (ch->senders.head) zyxrt_wait_wake(&ch->senders);
  return 1;
}

int zyxrt_chan_recv_ready(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  return ch->len > 0 || ch->closed;
}

int zyxrt_chan_send_ready(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  /* A send on a closed channel cannot proceed, so the arm is not ready and
     the select takes another one. A direct send panics instead, which is the
     rule for the operation rather than for the arm. */
  if (ch->closed) return 0;
  return ch->receivers.head != NULL || ch->len < ch->cap;
}

void zyxrt_chan_close(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (!ch || ch->closed) return;
  ch->closed = 1;
  zyxrt_wait_wake(&ch->receivers);
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
    zyxrt_wait_add(recv_arms[i] ? &ch->receivers : &ch->senders, &nodes[i]);
  }
  /* A select with a timeout also waits for the deadline. */
  zyxrt_waiter timer;
  int timed = deadline != 0;
  if (timed) {
    self->wake_at = deadline;
    timer.coro = self;
    timer.next = NULL;
    zyxrt_wait_add(zyxrt_sched_sleepers(), &timer);
  }
  zyxrt_sched_suspend(self);
  /* The lists that did not fire still hold the nodes: take them off. A wake
     empties the list it fired on, so those nodes are already gone. */
  for (i = 0; i < n; i++) {
    zyxrt_chan *ch = (zyxrt_chan *)handles[i];
    zyxrt_wait_remove(recv_arms[i] ? &ch->receivers : &ch->senders, &nodes[i]);
  }
  if (timed) zyxrt_wait_remove(zyxrt_sched_sleepers(), &timer);
  if (nodes != inline_nodes) free(nodes);
}
