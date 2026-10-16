/* Process entry and runtime bootstrap. The generated main calls zyxrt_run_main
   with the address of the zyx main function; the runtime runs it as the root
   coroutine on the first scheduler thread and stops when every thread has
   nothing left to do. */

#include "zyxrt_internal.h"

int zyxrt_run_main(zyxrt_main_fn fn, void *arg) {
  zyxrt_platform_init();
  zyxrt_timer_init();
  zyxrt_pool_warm();
  zyxrt_wakeup_init();
  zyxrt_block_start();
  zyxrt_loop_start();
  zyxrt_wheel_init();
  zyxrt_preempt_init();
  zyxrt_sched_init();
  zyxrt_coro *root = zyxrt_coro_spawn((zyxrt_fn)fn, arg);
  root->root = 1;
  /* The calling thread is a scheduler thread, so it runs coroutines rather
     than watching them. It returns when the work is over, which is when every
     thread has nothing left and nothing is on its way. */
  zyxrt_sched_run();
  zyxrt_sched_shutdown();
  return 0;
}
