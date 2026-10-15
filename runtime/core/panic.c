/* Panic reporting and process abort paths. A panic raises an exception and
   unwinds the coroutine that raised it with the Itanium C++ ABI's forced
   unwinding: every frame that owns cleanup work runs it on the way past,
   and the stop function is called at the end of the stack. A coroutine's
   stack is its own, so the end of that stack is the coroutine boundary. The
   panic therefore belongs to the coroutine and the process keeps running;
   only the root coroutine's panic ends the process, with the exit code the
   command line documents. */

#include "zyxrt_internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unwind.h>

/* The exception a panic raises. The message is copied, so it outlives the
   region the panicking coroutine owned. */
typedef struct zyxrt_panic {
  struct _Unwind_Exception exc;
  char *message;
  size_t length;
} zyxrt_exception;

/* The exception class, which spells ZYXPANIC. */
#define ZYXXRT_PANIC_CLASS 0x5A595850414E4943ULL

static _Unwind_Reason_Code panic_stop(int version, _Unwind_Action actions,
                                      _Unwind_Exception_Class class,
                                      struct _Unwind_Exception *exc,
                                      struct _Unwind_Context *ctx, void *arg);

void zyxrt_panic(const char *msg, size_t len) {
  zyxrt_exception *p = (zyxrt_exception *)calloc(1, sizeof(zyxrt_exception));
  if (!p) abort();
  p->message = (char *)malloc(len + 1);
  if (!p->message) abort();
  memcpy(p->message, msg, len);
  p->message[len] = '\0';
  p->length = len;
  p->exc.exception_class = ZYXXRT_PANIC_CLASS;
  p->exc.exception_cleanup = NULL;
  _Unwind_ForcedUnwind(&p->exc, panic_stop, p);
  /* panic_stop never returns. */
  abort();
}

/* panic_stop runs at the end of the coroutine's stack, once every cleanup
   frame has run. It reports the panic, hands it to the coroutine, and
   leaves the stack to the scheduler. */
static _Unwind_Reason_Code panic_stop(int version, _Unwind_Action actions,
                                      _Unwind_Exception_Class class,
                                      struct _Unwind_Exception *exc,
                                      struct _Unwind_Context *ctx, void *arg) {
  (void)version;
  (void)actions;
  (void)class;
  (void)ctx;
  zyxrt_exception *p = (zyxrt_exception *)arg;
  if (exc != NULL && exc != &p->exc) {
    p = (zyxrt_exception *)exc;
  }
  /* A panic is never silent: it goes to standard error, and the coroutine
     keeps it for whoever joins it. */
  fflush(stdout);
  fputs("panic: ", stderr);
  fwrite(p->message, 1, p->length, stderr);
  fputc('\n', stderr);
  fflush(stderr);
  zyxrt_coro *self = zyxrt_sched_current();
  if (self != NULL && !self->root) {
    self->panic = p;
    zyxrt_sched_abandon(self);
  }
  /* The root coroutine's panic ends the process. */
  exit(101);
}

const char *zyxrt_panic_message(const void *panic, size_t *len) {
  const zyxrt_exception *p = (const zyxrt_exception *)panic;
  if (p == NULL) {
    return NULL;
  }
  if (len != NULL) {
    *len = p->length;
  }
  return p->message;
}
