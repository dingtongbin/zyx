/* The trap a fatal fault is reported from, one implementation for every
   target. A fault that reaches the trap is one nothing else could handle: the
   machinery a panic unwinds with cannot be trusted when the fault is in code
   that was never compiled to work with it, and a fault on a stack that has run
   out has no room to unwind in. So the fault is not unwound, it is reported,
   which is what frames.c does; the trap only has to catch it and hand it over.

   Installing the trap is a process-wide act on Windows, where one handler
   serves every thread, and a per-thread act elsewhere, where the handler runs on
   the thread that faulted and needs a stack of its own to run on: a coroutine
   that runs out of stack faults on the very stack that ran out. The scheduler
   installs it on the thread a program begins on and on every thread it starts
   to run coroutines, because those are the threads a coroutine's stack is
   entered from. */

#include "zyxrt_internal.h"

#include <stdatomic.h>

#if defined(_WIN32)

#include <windows.h>

/* A fault that reaches the handler is one nothing else could handle, and the
   program cannot continue from it. */
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

void zyxrt_fault_install(void) {
  /* One handler serves the process, so the first thread to ask installs it and
     the rest have nothing left to do. */
  static _Atomic int installed;
  if (atomic_exchange_explicit(&installed, 1, memory_order_acq_rel)) return;
  AddVectoredExceptionHandler(1, fatal_handler);
}

#elif defined(__wasm__)

void zyxrt_fault_install(void) {}

#else

#include <signal.h>
#include <string.h>

/* The size a report is given to run in. It is a fixed size rather than the
   one signal.h names, because that name is a call on the systems that grow
   their stacks lazily, and this buffer is set up where a call is not wanted. */
#define ZYXXRT_FAULT_STACK (64u * 1024u)

static const char *fault_what(int sig) {
  switch (sig) {
  case SIGSEGV:
    return "a memory fault";
  case SIGBUS:
    return "a bus error";
  case SIGFPE:
    return "an arithmetic fault";
  case SIGILL:
    return "an illegal instruction";
  default:
    return "a fault";
  }
}

static void fault_handler(int sig, siginfo_t *info, void *context) {
  (void)context;
  zyxrt_fatal_report(fault_what(sig), info != NULL ? info->si_addr : NULL, NULL);
}

void zyxrt_fault_install(void) {
  static _Atomic int installed;
  /* The buffer belongs to the thread, because the report is written on the
     thread that faulted. */
  static _Thread_local char fault_stack[ZYXXRT_FAULT_STACK];
  stack_t stack;
  if (!atomic_exchange_explicit(&installed, 1, memory_order_acq_rel)) {
    struct sigaction action;
    memset(&action, 0, sizeof action);
    action.sa_sigaction = fault_handler;
    action.sa_flags = SA_SIGINFO | SA_ONSTACK;
    sigemptyset(&action.sa_mask);
    sigaction(SIGSEGV, &action, NULL);
    sigaction(SIGBUS, &action, NULL);
    sigaction(SIGFPE, &action, NULL);
    sigaction(SIGILL, &action, NULL);
  }
  stack.ss_sp = fault_stack;
  stack.ss_size = sizeof fault_stack;
  stack.ss_flags = 0;
  sigaltstack(&stack, NULL);
}

#endif
