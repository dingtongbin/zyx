/* Platform context layer: Windows uses the hand-written x86-64 stack switch,
   and the fiber API elsewhere. */

/* Winsock comes before the platform headers, because they include the older
   socket interface otherwise. */
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0600
#endif

#include <winsock2.h>

#include "zyxrt_internal.h"

#include <fcntl.h>
#include <io.h>
#include <stdio.h>

#if defined(__x86_64__)

#include "switch_ctx.h"

/* Windows keeps the bounds of the stack a thread runs on in the thread
   environment block, and its stack probe reads them to decide whether an
   allocation leaves the stack. A coroutine runs on a reservation of its own,
   so the bounds move with the stack that is in use: the thread's own while
   the scheduler runs and the coroutine's while it does. The fiber API does
   this itself, which is why a target that falls back to fibers needs none of
   it. */
static _Thread_local void *stack_bounds_base;
static _Thread_local void *stack_bounds_limit;
static _Thread_local int stack_bounds_known;

static void stack_bounds(void) {
  /* The first call happens before any coroutine runs on the thread, so what
     the block holds then is the thread's own stack. */
  if (!stack_bounds_known) {
    NT_TIB *tib = (NT_TIB *)NtCurrentTeb();
    stack_bounds_base = tib->StackBase;
    stack_bounds_limit = tib->StackLimit;
    stack_bounds_known = 1;
  }
}

void zyxrt_ctx_bounds_thread(void) {
  NT_TIB *tib;
  stack_bounds();
  tib = (NT_TIB *)NtCurrentTeb();
  tib->StackBase = stack_bounds_base;
  tib->StackLimit = stack_bounds_limit;
}

void zyxrt_ctx_bounds_coro(zyxrt_coro *c) {
  NT_TIB *tib;
  stack_bounds();
  tib = (NT_TIB *)NtCurrentTeb();
  tib->StackBase = (char *)c->stack + c->stack_size;
  tib->StackLimit = (char *)c->stack_limit;
}

#else

#include <windows.h>

void *zyxrt_ctx_thread(void) { return ConvertThreadToFiber(NULL); }

void *zyxrt_ctx_new(void *stack, size_t size) {
  (void)stack;
  return CreateFiber(size, (LPFIBER_START_ROUTINE)zyxrt_sched_enter, NULL);
}

/* A fiber is not given a stack again once it has run, so a context that goes
   back on a stack is a new one and the old is given up. */
void *zyxrt_ctx_reset(void *ctx, void *stack, size_t size) {
  if (ctx != NULL) DeleteFiber(ctx);
  return zyxrt_ctx_new(stack, size);
}

void zyxrt_ctx_switch(void *from, void *to) {
  (void)from;
  SwitchToFiber(to);
}

void zyxrt_ctx_free(void *ctx) { DeleteFiber(ctx); }

/* A fiber carries its own stack bounds, so there is nothing to update. */
void zyxrt_ctx_bounds_thread(void) {}
void zyxrt_ctx_bounds_coro(zyxrt_coro *c) { (void)c; }

#endif

/* The runtime writes UTF-8 bytes, so a line feed must stay one byte rather
   than becoming the two bytes of a text-mode line ending. The socket library is
   brought up here rather than with the event loop, because it is a property of
   the process: a program whose own C calls open sockets needs it whether or not
   a coroutine ever waits on a descriptor. */
void zyxrt_platform_init(void) {
  WSADATA data;
  _setmode(_fileno(stdout), _O_BINARY);
  _setmode(_fileno(stderr), _O_BINARY);
  if (WSAStartup(MAKEWORD(2, 2), &data) != 0) {
    zyxrt_panic("cannot start the socket library", 30);
  }
}

/* A Windows C runtime expects this symbol as soon as a translation unit
   touches floating point, and a compiler that links without the vendor
   startup must supply it. */
int _fltused = 0;
