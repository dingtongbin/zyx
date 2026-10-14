/* Panic reporting and process abort paths. A panic ends the process with
   the exit code the command line documents for a failed main coroutine. */

#include "zyxrt.h"

#include <stdio.h>
#include <stdlib.h>

void zyxrt_panic(const char *msg, size_t len) {
  fflush(stdout);
  fputs("panic: ", stderr);
  fwrite(msg, 1, len, stderr);
  fputc('\n', stderr);
  fflush(stderr);
  exit(101);
}
