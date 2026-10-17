/* Coroutine stacks. A stack is reserved in full and committed as it is used:
   a coroutine that uses little memory costs little memory, and one that uses
   much still has room, up to the reservation the language promises it.
 *
 * Stacks come from runs of slots in one reservation each, and a stack a program
 * no longer needs goes back on a free list rather than to the operating system,
 * because asking for a reservation and handing it back costs more than the
 * coroutine that used it does. A slot is a stack of the promised size plus a
 * page that stays uncommitted, which is what turns running off the end of a
 * stack into a fault when the frame that did it came from C and no check stood
 * in its way.
 *
 * A target whose kernel grows a stack by itself commits every page at once and
 * lets the kernel hand them out as they are touched. A target whose kernel does
 * not gets a stack that starts with the pages a new coroutine is expected to use
 * and grows in steps, which the check the compiler puts in every function
 * prologue drives; a frame that would pass the reservation's low end is the
 * coroutine running out of stack, which is a panic. */

#include "zyxrt_internal.h"

#include <stdlib.h>
#include <string.h>

/* The stack a new coroutine is committed up front: enough that its first frames
   need no growth, and little enough that a program with many coroutines pays
   for the stack they use rather than for the stack they might use. */
#define ZYXXRT_STACK_WARM (8u * 1024u)

/* What a growth commits on top of the frame that asked for it, so the next few
   descents cost nothing, and the room kept below that frame: the frames of the
   functions between one check and the check of what it calls are paid for out
   of the margin, so it covers the prologue of any callee and leaves room for
   the report a stack that runs out makes. */
#define ZYXXRT_STACK_STEP (64u * 1024u)
#define ZYXXRT_STACK_MARGIN (32u * 1024u)

/* How much one run of slots holds, which is what makes the cost of a coroutine
   its own work rather than a call of the operating system. */
#define ZYXXRT_ARENA_BYTES ((size_t)4u * 1024u * 1024u)

#if defined(_WIN32)

#include <windows.h>

size_t zyxrt_system_page(void) {
  SYSTEM_INFO info;
  GetSystemInfo(&info);
  return (size_t)info.dwPageSize;
}

static void *arena_reserve(size_t bytes) {
  return VirtualAlloc(NULL, bytes, MEM_RESERVE, PAGE_NOACCESS);
}

static int arena_commit(void *at, size_t bytes) {
  return VirtualAlloc(at, bytes, MEM_COMMIT, PAGE_READWRITE) != NULL;
}

#else

#include <sys/mman.h>
#include <unistd.h>

size_t zyxrt_system_page(void) {
  long page = sysconf(_SC_PAGESIZE);
  return page > 0 ? (size_t)page : 4096u;
}

/* The reservation is readable and writable throughout on a target whose kernel
   commits a page as it is touched, because there the stack is committed by
   being used; the guard page below each slot is made inaccessible again as the
   slot is carved. */
static void *arena_reserve(size_t bytes) {
  return mmap(NULL, bytes, PROT_READ | PROT_WRITE,
              MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
}

static int arena_commit(void *at, size_t bytes) {
  (void)at;
  (void)bytes;
  return 1;
}

#endif

/* A free slot, linked through its own top: a stack grows down from there, so
   the bytes the link lives in are the last the coroutine that takes the slot
   will use, by which time the link has been read. limit is how far the slot is
   committed, which is what a coroutine taking it starts with. */
typedef struct zyxrt_slot {
  struct zyxrt_slot *next;
  void *limit;
} zyxrt_slot;

static zyxrt_slot *free_slots;

/* slot_guard makes the page below a slot inaccessible, which is where a stack
   that runs past its reservation without a check faults. */
static void slot_guard(void *stack, size_t size) {
  (void)stack;
  (void)size;
#if !defined(_WIN32)
  size_t page = zyxrt_system_page();
  mprotect((char *)stack - page, page, PROT_NONE);
#endif
}

/* stack_run reserves a run of slots and puts each of them on the free list. */
static void stack_run(size_t size) {
  size_t page = zyxrt_system_page();
  size_t slot = size + page;
  size_t wanted = ZYXXRT_ARENA_BYTES / slot;
  if (wanted < 1) wanted = 1;
  char *base = (char *)arena_reserve(slot * wanted);
  if (base == NULL) {
    /* A reservation of its own is better than none, which is what a program
       asking for more coroutines than the run was sized for needs. */
    base = (char *)arena_reserve(slot);
    wanted = 1;
    if (base == NULL) return;
  }
  size_t warm = size < ZYXXRT_STACK_WARM ? size : ZYXXRT_STACK_WARM;
  for (size_t i = 0; i < wanted; i++) {
    char *stack = base + i * slot + page;
    if (!arena_commit(stack + size - warm, warm)) continue;
    slot_guard(stack, size);
    zyxrt_slot *s = (zyxrt_slot *)(stack + size - sizeof(zyxrt_slot));
    s->next = free_slots;
    s->limit = stack + size - warm;
    free_slots = s;
  }
}

void *zyxrt_stack_map(size_t size, void **limit) {
  if (free_slots == NULL) {
    stack_run(size);
    if (free_slots == NULL) {
      *limit = NULL;
      return NULL;
    }
  }
  zyxrt_slot *s = free_slots;
  free_slots = s->next;
  *limit = s->limit;
  /* The link lives at the top of the slot, which is where the stack begins. */
  s->next = NULL;
  s->limit = NULL;
  return (char *)s - (size - sizeof(zyxrt_slot));
}

void zyxrt_stack_unmap(void *stack, size_t size, void *limit) {
  zyxrt_slot *s = (zyxrt_slot *)((char *)stack + size - sizeof(zyxrt_slot));
  s->limit = limit;
  s->next = free_slots;
  free_slots = s;
}

/* slot_commit commits more of a slot, which is what a frame that reaches below
   what is committed needs. */
static void slot_commit(void *want, void *old_limit) {
  if (!arena_commit(want, (char *)old_limit - (char *)want)) {
    zyxrt_panic("stack overflow", 14);
  }
}

void zyxrt_stack_grow(void *low) {
  zyxrt_coro *c = zyxrt_sched_current();
  if (c == NULL || c->stack == NULL) return;
  uintptr_t lowest = (uintptr_t)c->stack;
  uintptr_t frame = (uintptr_t)low;
  uintptr_t want;
  if (frame >= (uintptr_t)c->stack_limit) return;
  if (frame < lowest + ZYXXRT_STACK_MARGIN) {
    /* The frame reaches the end of what the language promised this coroutine,
       so the stack has run out. What is committed is above it, which is where
       the report runs. */
    zyxrt_panic("stack overflow", 14);
  }
  want = frame - ZYXXRT_STACK_MARGIN;
  if (want > lowest + ZYXXRT_STACK_STEP) want -= ZYXXRT_STACK_STEP;
  /* Everything above the committed low end is committed already, so the run the
     frame needs is covered by committing up to where the committed part began. */
  slot_commit((void *)want, c->stack_limit);
  /* The record of the stack in use is what the next check reads and what a
     switch restores, so both are told where the committed part now ends. */
  c->stack_limit = (void *)want;
  zyxrt_ctx_bounds_coro(c);
}

/* zyxrt_stack_warm is not needed: a slot's first pages are committed when the
   slot is carved, and a page that is committed is handed out the first time it
   is written, which is where the coroutine that writes it is already going.
   Touching the pages here would commit them physically for a coroutine that may
   never use them. */

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with. */
size_t zyxrt_stack_size(void) {
  const char *configured = getenv("ZYX_STACK");
  if (configured != NULL && configured[0] != '\0') {
    char *end = NULL;
    unsigned long long value = strtoull(configured, &end, 10);
    if (end != configured && value >= ZYXXRT_STACK_WARM) {
      return (size_t)value;
    }
  }
  return ZYXXRT_STACK_DEFAULT;
}
