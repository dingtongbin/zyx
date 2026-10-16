/* Platform shims: WASM. A wasm target has no threads, so there is no wakeup
   handle and no blocking pool: the pool runs every call where it stands. */

#include "zyxrt_internal.h"

void zyxrt_wakeup_init(void) {}
void zyxrt_wakeup_signal(void) {}
void zyxrt_wakeup_signal_all(size_t count) { (void)count; }
void zyxrt_wakeup_wait(int64_t ns) { (void)ns; }

void zyxrt_platform_init(void) {}
