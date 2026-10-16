/* Platform shims: WASM. A wasm target has no threads, so there is no wakeup
   handle and no blocking pool: the pool runs every call where it stands. */

#include "zyxrt_internal.h"

void zyxrt_wakeup_init(void) {}
void zyxrt_wakeup_signal(void) {}
void zyxrt_wakeup_signal_all(size_t count) { (void)count; }
void zyxrt_wakeup_wait(int64_t ns) { (void)ns; }

void zyxrt_ctx_bounds_thread(void) {}
void zyxrt_ctx_bounds_coro(void *stack, size_t size) { (void)stack; (void)size; }

void zyxrt_platform_init(void) {}
