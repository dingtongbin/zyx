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
#include <unwind.h>

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

/* frame_entry answers the table entry of the function an address falls in: of
   the functions at or below it, the nearest one. Only addresses the program's
   own file holds are named, because that is what the tables describe. A program
   has a few thousand functions, so a walk is the right shape for a report. */
static const zyxrt_frame *frame_entry(uintptr_t address) {
  const zyxrt_frame *best = NULL;
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
        best = &frames[j];
        best_at = at;
      }
    }
  }
  return best;
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

#if defined(_WIN32)

/* report_bounds answers the low and high end of the stack the report is running
   on. The runtime keeps a coroutine's bounds in the thread environment block
   while it runs, so the answer is the stack really in use: a frame whose stack
   pointer is above the high end belongs to whatever ran the coroutine, and a
   report of a coroutine's panic is about the coroutine. */
static void report_bounds(uintptr_t *low, uintptr_t *high) {
  NT_TIB *tib = (NT_TIB *)NtCurrentTeb();
  *low = (uintptr_t)tib->StackLimit;
  *high = (uintptr_t)tib->StackBase;
}

/* Windows walks a stack itself. The system's capture answers addresses without
   the stack pointer each was taken in, and a report has to know when it has left
   the stack it is about: a coroutine's frames end where its stack ends, and what
   follows belongs to the scheduler that ran it. RtlVirtualUnwind is the unwinder
   the system's own debuggers use, so this walk reads what they read and writes
   nothing of its own into the report. Two frames are skipped, this function and
   the report that called it. */
size_t zyxrt_frames_capture(void **out, size_t max, void *above) {
  CONTEXT ctx;
  size_t count = 0;
  size_t skip = 2;
  uintptr_t low = 0;
  uintptr_t high = 0;
  (void)above;
  report_bounds(&low, &high);
  memset(&ctx, 0, sizeof ctx);
  RtlCaptureContext(&ctx);
  while (count < max && ctx.Rip != 0) {
    PRUNTIME_FUNCTION entry;
    ULONG64 image = 0;
    if (high != 0 && (uintptr_t)ctx.Rsp >= high) break;
    if (skip > 0) {
      skip--;
    } else {
      out[count++] = (void *)(uintptr_t)ctx.Rip;
    }
    entry = RtlLookupFunctionEntry(ctx.Rip, &image, NULL);
    if (entry == NULL) {
      /* A leaf function keeps no unwind data of its own, so its return address
         is at the top of its frame. */
      ctx.Rip = *(ULONG64 *)ctx.Rsp;
      ctx.Rsp += sizeof(void *);
      continue;
    }
    {
      void *handler = NULL;
      DWORD64 establisher = 0;
      RtlVirtualUnwind(0, image, ctx.Rip, entry, &ctx, &handler, &establisher, NULL);
    }
  }
  return count;
}

#else

/* report_bounds answers the low and high end of the stack the report is running
   on, which for a coroutine is its own stack: a frame whose unwind context is
   above the high end belongs to whatever ran the coroutine rather than to the
   coroutine. A report that runs where no coroutine does has no bounds. */
static void report_bounds(uintptr_t *low, uintptr_t *high) {
  zyxrt_coro *c = zyxrt_sched_current();
  *low = 0;
  *high = 0;
  if (c != NULL && c->stack != NULL) {
    *low = (uintptr_t)c->stack;
    *high = (uintptr_t)c->stack + c->stack_size;
  }
}

/* A walk records the addresses of the frames outside the marker the caller
   named, innermost first, and stops where the stack the report is about ends. */
typedef struct zyxrt_walk {
  void **out;
  size_t max;
  size_t count;
  void *above;
  int passed;
  uintptr_t low;
  uintptr_t high;
} zyxrt_walk;

static _Unwind_Reason_Code frame_step(struct _Unwind_Context *ctx, void *arg) {
  zyxrt_walk *walk = (zyxrt_walk *)arg;
  uintptr_t at = (uintptr_t)_Unwind_GetIP(ctx);
  uintptr_t cfa = (uintptr_t)_Unwind_GetCFA(ctx);
  if (at == 0) return _URC_END_OF_STACK;
  if (walk->above != NULL && !walk->passed) {
    /* The frame that holds the marker is the caller's own, so it and the frames
       inside it are the report rather than the program it reports on. */
    if (cfa <= (uintptr_t)walk->above) {
      return _URC_NO_REASON;
    }
    walk->passed = 1;
    return _URC_NO_REASON;
  }
  if (walk->high != 0 && cfa >= walk->high) return _URC_END_OF_STACK;
  if (walk->count >= walk->max) return _URC_END_OF_STACK;
  walk->out[walk->count++] = (void *)at;
  return _URC_NO_REASON;
}

size_t zyxrt_frames_capture(void **out, size_t max, void *above) {
  zyxrt_walk walk;
  walk.out = out;
  walk.max = max;
  walk.count = 0;
  walk.above = above;
  walk.passed = 0;
  report_bounds(&walk.low, &walk.high);
  _Unwind_Backtrace(frame_step, &walk);
  return walk.count;
}

#endif

/* write_frame writes one frame: the position the function was declared at and
   its name, and for a frame the program carries no name for, the address it
   stands at, which is what a report of a fault in code the program did not
   compile can offer. */
static void write_frame(uintptr_t at) {
  const zyxrt_frame *entry = frame_entry(at);
  write_text("  at ");
  if (entry != NULL && entry->where != NULL && entry->where[0] != '\0') {
    write_text(entry->where);
  } else {
    write_text("<unknown>");
  }
  write_text(" ");
  write_name(entry != NULL ? entry->name : NULL);
  write_text("\n");
}

void zyxrt_frames_print(void *const *frames, size_t count) {
  for (size_t i = 0; i < count; i++) {
    write_frame((uintptr_t)frames[i]);
  }
}

void zyxrt_fatal_report(const char *what, void *address, void *sp) {
  /* A fault is reported where the fault is, with the frames that led to it
     still on the stack, so the walk needs no marker to find them: a handler a
     system calls has its own way of arriving, and the frames a panic takes are
     the ones it walks while nothing has been unwound yet. */
  void *frames[32];
  size_t count;
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
  count = zyxrt_frames_capture(frames, sizeof frames / sizeof frames[0], NULL);
  write_text("frames:\n");
  zyxrt_frames_print(frames, count);
  write_text("\n");
  _exit(101);
}
