/* Crash reporting. A fault the program cannot continue from is not a panic: the
   machinery a panic uses to unwind cannot be trusted when the fault is in code
   that was never compiled to work with it, and a fault on a stack that has run
   out has no room to unwind anyway. What such a fault needs is to be read by a
   person, so this file writes one report to standard error: what happened, where,
   which coroutine it happened in, and the functions the stack passed through.
 *
 * The names come from tables the compiler emits, one entry per function a module
   defined, holding the address and the name. Registering them costs a store per
 * module before the program runs, and looking a name up costs a walk of a few
 * thousand entries, which is nothing at the moment a program is dying. Nothing
 * here allocates or takes a lock, because it runs where the state of the heap and
 * of the locks is not known. */

#include "zyxrt_internal.h"

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <unwind.h>

#if defined(_WIN32)

#include <windows.h>

static void report_write(const char *text, size_t length) {
  DWORD written = 0;
  WriteFile(GetStdHandle(STD_ERROR_HANDLE), text, (DWORD)length, &written, NULL);
}

/* in_program answers whether an address belongs to the program's own file. The
   tables describe that file's functions, so an address in a library the program
   is linked with must not be named by one of them. */
static int in_program(uintptr_t address) {
  MEMORY_BASIC_INFORMATION info;
  if (VirtualQuery((void *)address, &info, sizeof info) == 0) return 0;
  return info.AllocationBase == (void *)GetModuleHandleA(NULL);
}

#else

#include <dlfcn.h>
#include <unistd.h>

static void report_write(const char *text, size_t length) {
  ssize_t written = write(2, text, length);
  (void)written;
}

static int in_program(uintptr_t address) {
  Dl_info here;
  Dl_info self;
  if (dladdr((void *)address, &here) == 0) return 0;
  if (dladdr((void *)&zyxrt_fatal_report, &self) == 0) return 0;
  return here.dli_fbase == self.dli_fbase;
}

#endif

/* The tables, in registration order. Registration happens before the program
   runs (the compiler puts it in the constructor list) and the report happens
   after the program stops, so the count only ever grows and nothing is released. */
#define ZYXXRT_FRAME_TABLES 64

typedef struct zyxrt_frame_table {
  const zyxrt_frame *frames;
  size_t count;
} zyxrt_frame_table;

static zyxrt_frame_table tables[ZYXXRT_FRAME_TABLES];
static _Atomic size_t table_count;

void zyxrt_frames_register(const zyxrt_frame *frames, size_t count) {
  if (frames == NULL || count == 0) return;
  size_t at = atomic_fetch_add_explicit(&table_count, 1, memory_order_acq_rel);
  if (at >= ZYXXRT_FRAME_TABLES) return;
  tables[at].frames = frames;
  tables[at].count = count;
}

/* frame_name answers the name of the function an address falls in: of the
   functions at or below it, the nearest one. Only addresses the program's own
   file holds are named, because that is what the tables describe. A program has
   a few thousand functions, so a walk is the right shape for a report. */
static const char *frame_name(uintptr_t address) {
  const char *best = NULL;
  uintptr_t best_at = 0;
  size_t count;
  if (!in_program(address)) return NULL;
  count = atomic_load_explicit(&table_count, memory_order_acquire);
  if (count > ZYXXRT_FRAME_TABLES) count = ZYXXRT_FRAME_TABLES;
  for (size_t i = 0; i < count; i++) {
    const zyxrt_frame *frames = tables[i].frames;
    if (frames == NULL) continue;
    for (size_t j = 0; j < tables[i].count; j++) {
      uintptr_t at = (uintptr_t)frames[j].address;
      if (at == 0 || at > address) continue;
      if (best == NULL || at > best_at) {
        best = frames[j].name;
        best_at = at;
      }
    }
  }
  return best;
}

const char *zyxrt_frame_name(void *address) {
  if (address == NULL) return NULL;
  return frame_name((uintptr_t)address);
}

static void write_text(const char *text) { report_write(text, strlen(text)); }

static void write_hex(uintptr_t value) {
  char buffer[2 + 16];
  buffer[0] = '0';
  buffer[1] = 'x';
  for (int i = 0; i < 16; i++) {
    int digit = (int)((value >> (4 * (15 - i))) & 0xf);
    buffer[2 + i] = (char)(digit < 10 ? '0' + digit : 'a' + digit - 10);
  }
  report_write(buffer, sizeof(buffer));
}

static void write_name(const char *name) {
  if (name == NULL) {
    write_text("<unknown>");
    return;
  }
  write_text(name);
}

/* frame_step writes one frame of the stack the report walks. The unwinder stops
   at the end of the stack it is given, which for a coroutine is the end of that
   coroutine's own stack. A frame whose module was built with line information
   carries a name; one that was not is written as the address it is. */
static _Unwind_Reason_Code frame_step(struct _Unwind_Context *ctx, void *arg) {
  int *depth = (int *)arg;
  if (*depth >= 32) return _URC_END_OF_STACK;
  uintptr_t at = (uintptr_t)_Unwind_GetIP(ctx);
  if (at == 0) return _URC_END_OF_STACK;
  write_text("  ");
  write_name(frame_name(at));
  write_text("  at ");
  write_hex(at);
  write_text("\n");
  (*depth)++;
  return _URC_NO_REASON;
}

void zyxrt_fatal_report(const char *what, void *address, void *sp) {
  zyxrt_coro *self = zyxrt_sched_current();
  write_text("\nfatal: ");
  write_text(what);
  if (address != NULL) {
    write_text(" at ");
    write_hex((uintptr_t)address);
  }
  if (sp != NULL) {
    write_text(" (stack ");
    write_hex((uintptr_t)sp);
    write_text(")");
  }
  write_text("\n");
  if (self != NULL) {
    write_text("coroutine ");
    write_hex((uintptr_t)self);
    if (self->root) write_text(" (the program's own)");
    write_text("\n");
  }
  write_text("frames:\n");
  {
    int depth = 0;
    _Unwind_Backtrace(frame_step, &depth);
  }
  write_text("\n");
  _exit(101);
}
