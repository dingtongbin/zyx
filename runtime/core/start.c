/* Process entry and runtime bootstrap. The generated main calls zyxrt_run_main
   with the address of the zyx main function; the runtime runs it as the
   root coroutine and drains the coroutines nobody joined. */

#include "zyxrt_internal.h"

int zyxrt_run_main(zyxrt_main_fn fn, void *arg) {
  zyxrt_platform_init();
  zyxrt_timer_init();
  zyxrt_pool_warm();
  zyxrt_sched_init();
  zyxrt_coro *root = zyxrt_coro_spawn((zyxrt_fn)fn, arg);
  root->root = 1;
  for (;;) {
    if (zyxrt_sched_run_once()) {
      continue;
    }
    /* Nothing is ready. A sleeping coroutine may be due, and otherwise the
       scheduler waits for the earliest deadline rather than spinning. */
    int64_t wait = zyxrt_sched_wait();
    if (wait < 0) {
      break;
    }
    if (wait > 0) {
      zyxrt_sleep((uint64_t)wait);
    }
  }
  zyxrt_sched_shutdown();
  return 0;
}
