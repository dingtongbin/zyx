/* Platform context layer: android uses the hand-written x86-64 stack switch, and the portable context routines elsewhere. */

#include "zyxrt_internal.h"
#include "wakeup.h"

#if defined(__x86_64__)
#include "switch_ctx.h"
#else
#include "posix_ctx.h"
#endif

void zyxrt_platform_init(void) {}
