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

/* What a coroutine of each tier commits up front, how far it reaches when it
   needs more, and the room kept below the frame that asked, so that the frames
   of the functions between one check and the check of what it calls are paid for
   out of it. A packed coroutine is one that is expected to use a few kilobytes
   and to call only C whose frames are small; a full one may reach any library,
   so it starts with room and grows in larger steps. */
#define ZYXXRT_STACK_PACKED_WARM (1u * 1024u)
#define ZYXXRT_STACK_PACKED_STEP (2u * 1024u)
#define ZYXXRT_STACK_PACKED_MARGIN (1u * 1024u)
#define ZYXXRT_STACK_FULL_WARM (32u * 1024u)
#define ZYXXRT_STACK_FULL_STEP (64u * 1024u)
#define ZYXXRT_STACK_FULL_MARGIN (32u * 1024u)

static size_t tier_warm(int tier) {
  return tier == ZYXXRT_STACK_FULL ? ZYXXRT_STACK_FULL_WARM : ZYXXRT_STACK_PACKED_WARM;
}

static size_t tier_step(int tier) {
  return tier == ZYXXRT_STACK_FULL ? ZYXXRT_STACK_FULL_STEP : ZYXXRT_STACK_PACKED_STEP;
}

static size_t tier_margin(int tier) {
  return tier == ZYXXRT_STACK_FULL ? ZYXXRT_STACK_FULL_MARGIN : ZYXXRT_STACK_PACKED_MARGIN;
}

/* How many slots one run holds at least, and how much one run holds at most
   before another is asked for, which is what makes the cost of a coroutine its
   own work rather than a call of the operating system. */
#define ZYXXRT_ARENA_SLOTS 64
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

/* The free list is shared by every lane: a coroutine may be started on one
   thread and released by another, so what hands a slot out and takes one back is
   a short critical section. What is committed for the stacks of coroutines is
   counted, because that is the number a program that keeps many of them alive
   watches. */
static zyxrt_spin slot_lock;
static _Atomic size_t committed_stacks;

size_t zyxrt_stack_committed(void) {
  return atomic_load_explicit(&committed_stacks, memory_order_relaxed);
}

static void slot_commit(void *want, void *old_limit);

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

/* stack_run reserves a run of slots and puts each of them on the free list. The
   run holds enough slots that asking for a reservation is worth doing: a slot
   the size the language promises would fill a small run in three of them. What
   each slot is committed to is what the tier asks, which is what a program of
   many coroutines pays for each of them. */
static void stack_run(size_t size, int tier) {
  size_t page = zyxrt_system_page();
  size_t slot = size + page;
  size_t wanted = ZYXXRT_ARENA_SLOTS;
  size_t warm = tier_warm(tier);
  if (warm > size) warm = size;
  if (slot * wanted < ZYXXRT_ARENA_BYTES) {
    wanted = ZYXXRT_ARENA_BYTES / slot + 1;
  }
  char *base = (char *)arena_reserve(slot * wanted);
  if (base == NULL) {
    /* A reservation of its own is better than none, which is what a program
       asking for more coroutines than the run was sized for needs. */
    base = (char *)arena_reserve(slot);
    wanted = 1;
    if (base == NULL) return;
  }
  for (size_t i = 0; i < wanted; i++) {
    char *stack = base + i * slot + page;
    if (!arena_commit(stack + size - warm, warm)) continue;
    atomic_fetch_add_explicit(&committed_stacks, warm, memory_order_relaxed);
    slot_guard(stack, size);
    zyxrt_slot *s = (zyxrt_slot *)(stack + size - sizeof(zyxrt_slot));
    s->next = free_slots;
    s->limit = stack + size - warm;
    free_slots = s;
  }
}

void *zyxrt_stack_map(size_t size, int tier, void **limit) {
  size_t warm = tier_warm(tier);
  char *stack;
  char *warm_limit;
  zyxrt_slot *s;
  zyxrt_spin_hold(&slot_lock);
  s = free_slots;
  if (s != NULL) free_slots = s->next;
  zyxrt_spin_drop(&slot_lock);
  if (s == NULL) {
    stack_run(size, tier);
    zyxrt_spin_hold(&slot_lock);
    s = free_slots;
    if (s != NULL) free_slots = s->next;
    zyxrt_spin_drop(&slot_lock);
    if (s == NULL) {
      *limit = NULL;
      return NULL;
    }
  }
  if (warm > size) warm = size;
  stack = (char *)s - (size - sizeof(zyxrt_slot));
  warm_limit = stack + size - warm;
  if ((char *)s->limit > warm_limit) {
    /* The slot was carved for a smaller window, so what this tier asks for is
       committed now. */
    slot_commit(warm_limit, s->limit);
    *limit = warm_limit;
  } else {
    *limit = s->limit;
  }
  /* The link lives at the top of the slot, which is where the stack begins. */
  s->next = NULL;
  s->limit = NULL;
  return stack;
}

void zyxrt_stack_unmap(void *stack, size_t size, void *limit) {
  zyxrt_slot *s = (zyxrt_slot *)((char *)stack + size - sizeof(zyxrt_slot));
  s->limit = limit;
  zyxrt_spin_hold(&slot_lock);
  s->next = free_slots;
  free_slots = s;
  zyxrt_spin_drop(&slot_lock);
}

/* slot_commit commits more of a slot, which is what a frame that reaches below
   what is committed needs. */
static void slot_commit(void *want, void *old_limit) {
  size_t bytes = (char *)old_limit - (char *)want;
  if (!arena_commit(want, bytes)) {
    zyxrt_panic("stack overflow", 14);
  }
  atomic_fetch_add_explicit(&committed_stacks, bytes, memory_order_relaxed);
}

void zyxrt_stack_grow(void *low) {
  zyxrt_coro *c = zyxrt_sched_current();
  size_t margin;
  size_t step;
  uintptr_t lowest;
  uintptr_t frame;
  uintptr_t want;
  if (c == NULL || c->stack == NULL) return;
  margin = tier_margin(c->stack_tier);
  step = tier_step(c->stack_tier);
  lowest = (uintptr_t)c->stack;
  frame = (uintptr_t)low;
  if (frame >= (uintptr_t)c->stack_limit) return;
  if (frame < lowest + margin) {
    /* The frame reaches the end of what the language promised this coroutine,
       so the stack has run out. What is committed is above it, which is where
       the report runs. */
    zyxrt_panic("stack overflow", 14);
  }
  want = frame - margin;
  if (want > lowest + step) want -= step;
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

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with.
   ZYX_STACK names it in bytes, and it is read once: a coroutine asks for its
   stack on the way to being made, and reading the environment each time is work
   the coroutine pays for. */
size_t zyxrt_stack_size(void) {
  static size_t chosen;
  if (chosen != 0) return chosen;
  const char *configured = getenv("ZYX_STACK");
  if (configured != NULL && configured[0] != '\0') {
    char *end = NULL;
    unsigned long long value = strtoull(configured, &end, 10);
    if (end != configured && value >= ZYXXRT_STACK_FULL_WARM) {
      chosen = (size_t)value;
      return chosen;
    }
  }
  chosen = ZYXXRT_STACK_DEFAULT;
  return chosen;
}
