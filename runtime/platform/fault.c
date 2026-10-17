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

/* The exceptions the trap reports itself: a fault of the program, which no one
   can carry on from. The trap claims these because it is the only handler that
   runs where they happen. A fault on a coroutine's stack is one the dispatch does
   not walk to a place it accepts as unhandled, so the filter below is reached
   there only when the stack is the thread's own; that was measured with the bounds
   of the coroutine's stack, a terminating frame at its top, and unwind information
   for it all in place. A fault of the program is reported from the trap, on any
   stack.

   An exception the language does not name - a C++ library's own, the
   thread-naming exception a debugger's convention uses, a platform's - is left to
   the dispatch, which is what a library that raises and catches its own exception
   needs: a VEH runs before the dispatch, so claiming an exception there takes it
   away from the handler the library wrote. Such an exception that reaches the top
   with no handler at all is reported by the filter below, which the system calls
   for it: on the main thread, and on a thread the runtime started where the call
   that raised it ran on the thread's own stack. */
static int fault_of_the_program(DWORD code) {
  switch (code) {
  case EXCEPTION_ACCESS_VIOLATION:
  case EXCEPTION_IN_PAGE_ERROR:
  case EXCEPTION_ILLEGAL_INSTRUCTION:
  case EXCEPTION_PRIV_INSTRUCTION:
  case EXCEPTION_INT_DIVIDE_BY_ZERO:
  case EXCEPTION_FLT_DIVIDE_BY_ZERO:
  case EXCEPTION_INT_OVERFLOW:
  case EXCEPTION_FLT_INVALID_OPERATION:
  case EXCEPTION_FLT_OVERFLOW:
  case EXCEPTION_ARRAY_BOUNDS_EXCEEDED:
  case EXCEPTION_STACK_OVERFLOW:
    return 1;
  default:
    return 0;
  }
}

/* The faults that are not a fault of the program: a debugger's business, and the
   C runtime's own abort, which brings its message with it. */
static int reported_by_another(DWORD code) {
  return code == EXCEPTION_BREAKPOINT || code == EXCEPTION_SINGLE_STEP || code == 0x40000015u;
}

static void report(PEXCEPTION_POINTERS info) {
  DWORD code = info->ExceptionRecord->ExceptionCode;
  void *address = NULL;
  if ((code == EXCEPTION_ACCESS_VIOLATION || code == EXCEPTION_IN_PAGE_ERROR) &&
      info->ExceptionRecord->NumberParameters >= 2) {
    address = (void *)info->ExceptionRecord->ExceptionInformation[1];
  }
  zyxrt_fatal_report(fault_what(code), address, (void *)info->ContextRecord->Rsp);
}

static LONG CALLBACK fatal_handler(PEXCEPTION_POINTERS info) {
  if (!fault_of_the_program(info->ExceptionRecord->ExceptionCode)) {
    return EXCEPTION_CONTINUE_SEARCH;
  }
  report(info);
  return EXCEPTION_EXECUTE_HANDLER;
}

/* The backstop for what the trap does not claim: an exception that reached the top
   of a thread the system walks with no handler at all is reported the same way,
   rather than ending the process in silence. It reaches the main thread and the
   threads the runtime starts, whose frames are the thread's own; it is not reached
   when the exception is raised on a coroutine's stack, which the dispatch of this
   system does not walk to a top it accepts as unhandled, measured with the bounds
   of the stack, a terminating frame, and unwind information for it in place. An
   exception there is therefore one the trap has to claim if it is to be reported,
   which is why only the faults of the program are claimed. */
static LONG WINAPI unhandled_handler(PEXCEPTION_POINTERS info) {
  if (reported_by_another(info->ExceptionRecord->ExceptionCode)) {
    return EXCEPTION_CONTINUE_SEARCH;
  }
  report(info);
  return EXCEPTION_EXECUTE_HANDLER;
}

void zyxrt_fault_install(void) {
  /* One handler serves the process, so the first thread to ask installs it and
     the rest have nothing left to do. */
  static _Atomic int installed;
  if (atomic_exchange_explicit(&installed, 1, memory_order_acq_rel)) return;
  AddVectoredExceptionHandler(1, fatal_handler);
  SetUnhandledExceptionFilter(unhandled_handler);
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
