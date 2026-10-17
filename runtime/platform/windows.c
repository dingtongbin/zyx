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

void zyxrt_ctx_switch(void *from, void *to) {
  (void)from;
  SwitchToFiber(to);
}

void zyxrt_ctx_free(void *ctx) { DeleteFiber(ctx); }

/* A fiber carries its own stack bounds, so there is nothing to update. */
void zyxrt_ctx_bounds_thread(void) {}
void zyxrt_ctx_bounds_coro(zyxrt_coro *c) { (void)c; }

#endif

/* A fault that reaches the handler is one nothing else could handle, and the
   program cannot continue from it, so it is reported where it is and the process
   ends. The report names what happened; a fault the runtime itself uses raises no
   exception of its own, so nothing here stands in the way of a panic. */
static const char *fault_what(DWORD code) {
  switch (code) {
  case EXCEPTION_ACCESS_VIOLATION:
  case EXCEPTION_IN_PAGE_ERROR:
    return "a memory fault";
  case EXCEPTION_ILLEGAL_INSTRUCTION:
    return "an illegal instruction";
  case EXCEPTION_INT_DIVIDE_BY_ZERO:
  case EXCEPTION_FLT_DIVIDE_BY_ZERO:
    return "a division by zero";
  case EXCEPTION_STACK_OVERFLOW:
    return "a stack that ran out";
  case EXCEPTION_PRIV_INSTRUCTION:
    return "a privileged instruction";
  case EXCEPTION_INT_OVERFLOW:
    return "an integer that overflowed";
  case EXCEPTION_FLT_INVALID_OPERATION:
  case EXCEPTION_FLT_OVERFLOW:
    return "an invalid floating point operation";
  case EXCEPTION_ARRAY_BOUNDS_EXCEEDED:
    return "an access outside an array";
  default:
    return "an unhandled exception";
  }
}

static LONG CALLBACK fatal_handler(PEXCEPTION_POINTERS info) {
  DWORD code = info->ExceptionRecord->ExceptionCode;
  void *address = NULL;
  if (code == EXCEPTION_BREAKPOINT || code == EXCEPTION_SINGLE_STEP) {
    /* A debugger's business, not a fault of the program. */
    return EXCEPTION_CONTINUE_SEARCH;
  }
  if (code == 0x40000015u) {
    /* The C runtime's own abort, which brings its message with it. */
    return EXCEPTION_CONTINUE_SEARCH;
  }
  if ((code == EXCEPTION_ACCESS_VIOLATION || code == EXCEPTION_IN_PAGE_ERROR) &&
      info->ExceptionRecord->NumberParameters >= 2) {
    address = (void *)info->ExceptionRecord->ExceptionInformation[1];
  }
  zyxrt_fatal_report(fault_what(code), address, (void *)info->ContextRecord->Rsp);
  return EXCEPTION_EXECUTE_HANDLER;
}

/* The runtime writes UTF-8 bytes, so a line feed must stay one byte rather
   than becoming the two bytes of a text-mode line ending. The socket library is
   brought up here rather than with the event loop, because it is a property of
   the process: a program whose own C calls open sockets needs it whether or not
   a coroutine ever waits on a descriptor. */
void zyxrt_platform_init(void) {
  WSADATA data;
  _setmode(_fileno(stdout), _O_BINARY);
  _setmode(_fileno(stderr), _O_BINARY);
  AddVectoredExceptionHandler(1, fatal_handler);
  if (WSAStartup(MAKEWORD(2, 2), &data) != 0) {
    zyxrt_panic("cannot start the socket library", 30);
  }
}

/* A Windows C runtime expects this symbol as soon as a translation unit
   touches floating point, and a compiler that links without the vendor
   startup must supply it. */
int _fltused = 0;
