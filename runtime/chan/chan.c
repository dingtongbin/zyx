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
  zyxrt_coro *senders;
  zyxrt_coro *receivers;
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
    if (ch->receivers) {
      push(ch, value);
      zyxrt_sched_wake(&ch->receivers);
      return;
    }
    if (ch->len < ch->cap) {
      push(ch, value);
      return;
    }
    /* The channel is full: suspend until a receiver drains it. */
    zyxrt_coro *self = zyxrt_sched_current();
    if (!self) zyxrt_panic("a channel blocks outside a coroutine", 36);
    self->wait_next = ch->senders;
    ch->senders = self;
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_recv(void *handle, void *out) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  for (;;) {
    if (ch->len > 0) {
      pop(ch, out);
      if (ch->senders) zyxrt_sched_wake(&ch->senders);
      return 1;
    }
    if (ch->closed) return 0;
    zyxrt_coro *self = zyxrt_sched_current();
    if (!self) zyxrt_panic("a channel blocks outside a coroutine", 36);
    self->wait_next = ch->receivers;
    ch->receivers = self;
    zyxrt_sched_suspend(self);
  }
}

int zyxrt_chan_try_send(void *handle, const void *value) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (ch->closed) return 0;
  if (ch->receivers) {
    push(ch, value);
    zyxrt_sched_wake(&ch->receivers);
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
  if (ch->senders) zyxrt_sched_wake(&ch->senders);
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
  return ch->receivers != NULL || ch->len < ch->cap;
}

void zyxrt_chan_close(void *handle) {
  zyxrt_chan *ch = (zyxrt_chan *)handle;
  if (!ch || ch->closed) return;
  ch->closed = 1;
  zyxrt_sched_wake(&ch->receivers);
}
