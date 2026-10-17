/* Process entry and runtime bootstrap. The generated main calls zyxrt_run_main
   with the address of the zyx main function; the runtime runs it as the root
   coroutine on the first scheduler thread and stops when every thread has
   nothing left to do. */

#include "zyxrt_internal.h"

int zyxrt_run_main(zyxrt_main_fn fn, void *arg) {
  zyxrt_platform_init();
  zyxrt_fault_install();
  zyxrt_timer_init();
  zyxrt_pool_warm();
  zyxrt_block_start();
  zyxrt_loop_start();
  zyxrt_wheel_init();
  zyxrt_preempt_start();
  zyxrt_sched_init();
  zyxrt_coro *root = zyxrt_coro_spawn((zyxrt_fn)fn, arg, ZYXXRT_STACK_FULL);
  root->root = 1;
  /* The calling thread is a scheduler thread, so it runs coroutines rather
     than watching them. It returns when the work is over, which is when every
     thread has nothing left and nothing is on its way. */
  zyxrt_sched_run();
  /* The threads the runtime started are still running, and this thread ends the
     process, so each of them is stopped and waited for first: a pool thread
     may be inside the call it was given, the poller inside the kernel, and the
     monitor between two looks, and ending the process under any of them races
     with the output and with the thread the runtime itself is made of. */
  zyxrt_sched_shutdown();
  zyxrt_block_stop();
  zyxrt_loop_stop();
  zyxrt_preempt_stop();
  return zyxrt_panic_code();
}
