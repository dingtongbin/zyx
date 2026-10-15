/* Coroutine stacks. A stack is reserved in full and committed page by page
   as it is touched, so a coroutine that uses little memory costs little
   memory and one that uses much still has room. The page below the stack is
   inaccessible, so running off the end faults instead of damaging whatever
   follows it.
 *
 * A stack is created with a bounded, deterministic warm-up: the pages the
 * language documents as the starting stack are committed once, at creation,
 * so the first use of a coroutine does not pay for them. Nothing beyond that
 * is warmed, because committing pages a program never touches is exactly
 * what the reservation is there to avoid. */

#include "zyxrt_internal.h"

#include <stdlib.h>
#include <string.h>

#if defined(_WIN32)

#include <windows.h>

/* The page size of the running system. */
static size_t system_page(void) {
  SYSTEM_INFO info;
  GetSystemInfo(&info);
  return (size_t)info.dwPageSize;
}

void *zyxrt_stack_map(size_t size) {
  size_t page = system_page();
  size_t total = size + page;
  char *base = (char *)VirtualAlloc(NULL, total, MEM_RESERVE, PAGE_NOACCESS);
  if (base == NULL) {
    return NULL;
  }
  if (VirtualAlloc(base + page, size, MEM_COMMIT, PAGE_READWRITE) == NULL) {
    VirtualFree(base, 0, MEM_RELEASE);
    return NULL;
  }
  return base + page;
}

void zyxrt_stack_unmap(void *stack, size_t size) {
  size_t page = system_page();
  VirtualFree((char *)stack - page, 0, MEM_RELEASE);
  (void)size;
}

#else

#include <sys/mman.h>
#include <unistd.h>

/* The page size of the running system. */
static size_t system_page(void) {
  long page = sysconf(_SC_PAGESIZE);
  return page > 0 ? (size_t)page : 4096u;
}

void *zyxrt_stack_map(size_t size) {
  size_t page = system_page();
  size_t total = size + page;
  void *base = mmap(NULL, total, PROT_NONE, MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
  if (base == MAP_FAILED) {
    return NULL;
  }
  if (mprotect((char *)base + page, size, PROT_READ | PROT_WRITE) != 0) {
    munmap(base, total);
    return NULL;
  }
  return (char *)base + page;
}

void zyxrt_stack_unmap(void *stack, size_t size) {
  size_t page = system_page();
  munmap((char *)stack - page, size + page);
}

#endif

/* ZYXXRT_STACK_WARM is the stack the language documents as the starting one,
   which a new coroutine commits up front. */
#define ZYXXRT_STACK_WARM (2u * 1024u)

/* zyxrt_stack_warm commits the pages a new coroutine is expected to use, so
   that its first run meets no page faults. It touches one byte per page and
   nothing else, which is the least work that commits a page. */
void zyxrt_stack_warm(void *stack, size_t size) {
  size_t page = system_page();
  size_t warm = size < ZYXXRT_STACK_WARM ? size : ZYXXRT_STACK_WARM;
  volatile unsigned char *top = (volatile unsigned char *)stack + size;
  for (size_t done = 0; done < warm; done += page) {
    top[-(ptrdiff_t)done - 1] = 0;
  }
}

/* zyxrt_stack_size returns the size a coroutine's stack is reserved with.
   ZYX_STACK names it in bytes. */
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
