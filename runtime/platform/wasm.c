/* Platform shims: WASM. A wasm target has no threads, so there is no blocking
   pool and nothing ever waits on a note: the pool runs every call where it
   stands. */

#include "zyxrt_internal.h"

void zyxrt_ctx_bounds_thread(void) {}
void zyxrt_ctx_bounds_coro(void *stack, size_t size) { (void)stack; (void)size; }

void zyxrt_platform_init(void) {}
