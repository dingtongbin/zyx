/* Platform context layer: the ucontext primitives, which every POSIX target
   uses as the portable stack switch. */

#ifndef ZYXRUNTIME_POSIX_CTX_H
#define ZYXRUNTIME_POSIX_CTX_H

#define _XOPEN_SOURCE 700

#include "zyxrt_internal.h"

#include <stdlib.h>
#include <ucontext.h>

/* A context is a ucontext_t plus the scheduler context it returns to. */
typedef struct zyxrt_posix_ctx {
  ucontext_t self;
  ucontext_t back;
} zyxrt_posix_ctx;

void *zyxrt_ctx_thread(void) { return NULL; }

void zyxrt_platform_init(void) {}

void *zyxrt_ctx_new(void *stack, size_t size, void (*entry)(void)) {
  zyxrt_posix_ctx *c = (zyxrt_posix_ctx *)calloc(1, sizeof(zyxrt_posix_ctx));
  if (!c) zyxrt_panic("out of memory", 13);
  getcontext(&c->self);
  c->self.uc_stack.ss_sp = stack;
  c->self.uc_stack.ss_size = size;
  c->self.uc_link = NULL;
  makecontext(&c->self, entry, 0);
  return c;
}

void zyxrt_ctx_switch(void *from, void *to) {
  zyxrt_posix_ctx *a = (zyxrt_posix_ctx *)from;
  zyxrt_posix_ctx *b = (zyxrt_posix_ctx *)to;
  swapcontext(&a->self, &b->self);
}

void zyxrt_ctx_free(void *ctx) { free(ctx); }

#endif /* ZYXRUNTIME_POSIX_CTX_H */
