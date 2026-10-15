/* Platform context layer: Windows fibers, which switch stacks without
   touching the registers by hand. */

#include "zyxrt_internal.h"

#include <fcntl.h>
#include <io.h>
#include <stdio.h>
#include <windows.h>

void zyxrt_platform_init(void) {
  /* The runtime writes UTF-8 bytes, so a line feed must stay one byte
     rather than becoming the two bytes of a text-mode line ending. */
  _setmode(_fileno(stdout), _O_BINARY);
  _setmode(_fileno(stderr), _O_BINARY);
}

/* A Windows C runtime expects this symbol as soon as a translation unit
   touches floating point, and a compiler that links without the vendor
   startup must supply it. */
int _fltused = 0;

void *zyxrt_ctx_thread(void) { return ConvertThreadToFiber(NULL); }

void *zyxrt_ctx_new(void *stack, size_t size, void (*entry)(void)) {
  (void)stack;
  return CreateFiber(size, (LPFIBER_START_ROUTINE)entry, NULL);
}

void zyxrt_ctx_switch(void *from, void *to) {
  (void)from;
  SwitchToFiber(to);
}

void zyxrt_ctx_free(void *ctx) { DeleteFiber(ctx); }
