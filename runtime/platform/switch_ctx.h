/* Platform context layer: the hand-written stack switch.
 *
 * A context is one word: the stack pointer of a suspended coroutine. The
 * switch saves the callee-saved registers on that stack, records the stack
 * pointer, loads the other coroutine's stack pointer, restores its
 * registers, and returns into it. No system call takes part, so a handoff
 * costs a few instructions rather than the signal-mask call the portable
 * context routines make. */

#ifndef ZYXRUNTIME_SWITCH_CTX_H
#define ZYXRUNTIME_SWITCH_CTX_H

#include "zyxrt_internal.h"

#include <stdlib.h>

/* The switch takes two contexts, each a pointer to the slot that holds a
   coroutine's stack pointer. */
void zyxrt_ctx_switch(void *from, void *to);
void zyxrt_ctx_enter(void);
extern const size_t zyxrt_ctx_saved_words;
extern const size_t zyxrt_ctx_saved_bytes;

/* new_slot returns a context, which is a slot holding a stack pointer. */
static void *new_slot(void) {
  void **slot = (void **)calloc(1, sizeof(void *));
  if (!slot) zyxrt_panic("out of memory", 13);
  return slot;
}

void *zyxrt_ctx_thread(void) { return new_slot(); }

/* The first frame is laid out exactly as the switch restores it: the vector
   state, then the callee-saved registers, then the address the switch returns
   to. The stack pointer is aligned so that the entry runs with the alignment the
   ABI expects after a call. */
static void slot_reset(void *ctx, void *stack, size_t size) {
  size_t total = zyxrt_ctx_saved_bytes + (zyxrt_ctx_saved_words + 1) * sizeof(void *);
  uintptr_t top = (uintptr_t)stack + size;
  /* The switch restores the registers with the stack pointer it recorded, so
     the first frame must sit where a saved one would. */
  uintptr_t sp = (top - total) & ~(uintptr_t)15;
  void **words = (void **)(sp + zyxrt_ctx_saved_bytes);
  for (size_t i = 0; i < zyxrt_ctx_saved_words; i++) {
    words[i] = NULL;
  }
  words[zyxrt_ctx_saved_words] = (void *)zyxrt_ctx_enter;
  *(void **)ctx = (void *)sp;
}

void *zyxrt_ctx_new(void *stack, size_t size) {
  void *slot = new_slot();
  slot_reset(slot, stack, size);
  return slot;
}

void *zyxrt_ctx_reset(void *ctx, void *stack, size_t size) {
  if (ctx == NULL) ctx = new_slot();
  slot_reset(ctx, stack, size);
  return ctx;
}

void zyxrt_ctx_free(void *ctx) { free(ctx); }

#endif
