/* Process entry and runtime bootstrap. The generated main calls zyxrt_run_main
   with the address of the zyx main function; the runtime runs it as the
   root coroutine and drains the coroutines nobody joined. */

#include "zyxrt_internal.h"

int zyxrt_run_main(zyxrt_main_fn fn, void *arg) {
  zyxrt_platform_init();
  zyxrt_sched_init();
  zyxrt_coro *root = zyxrt_coro_spawn((zyxrt_fn)fn, arg);
  root->root = 1;
  while (zyxrt_sched_run_once()) {
  }
  zyxrt_sched_shutdown();
  return 0;
}
