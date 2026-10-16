/* Process entry and runtime bootstrap. The generated main calls zyxrt_run_main
   with the address of the zyx main function; the runtime runs it as the
   root coroutine and drains the coroutines nobody joined. */

#include "zyxrt_internal.h"

int zyxrt_run_main(zyxrt_main_fn fn, void *arg) {
  zyxrt_platform_init();
  zyxrt_timer_init();
  zyxrt_pool_warm();
  zyxrt_wakeup_init();
  zyxrt_block_start();
  zyxrt_loop_start();
  zyxrt_sched_init();
  zyxrt_coro *root = zyxrt_coro_spawn((zyxrt_fn)fn, arg);
  root->root = 1;
  for (;;) {
    if (zyxrt_sched_run_once()) {
      continue;
    }
    /* Work another thread finished, and descriptors that became ready, are
       readied before the scheduler decides whether it has anything left to
       do. */
    zyxrt_sched_drain();
    zyxrt_loop_drain();
    if (zyxrt_sched_run_once()) {
      continue;
    }
    /* Nothing is ready. A sleeping coroutine may be due, a coroutine may
       wait for something outside the scheduler, and otherwise the scheduler
       waits for the earliest deadline rather than spinning. */
    int64_t wait = zyxrt_sched_wait();
    if (wait == ZYXRT_WAIT_DONE) {
      break;
    }
    if (wait == ZYXRT_WAIT_EXTERNAL) {
      zyxrt_wakeup_wait(-1);
      continue;
    }
    if (wait > 0) {
      zyxrt_wakeup_wait(wait);
    }
  }
  zyxrt_sched_shutdown();
  return 0;
}
