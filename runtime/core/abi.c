/* C ABI helpers shared by generated code: the output helpers the standard
   library calls, so a str crosses the boundary as a borrowed pointer and
   its length rather than as a NUL-terminated buffer.

   A value is printed by rendering it with the shared formatter and writing
   the bytes, which is the same rule the buffer writers use. */

#include "zyxrt.h"

#include <stdio.h>

void zyxrt_print_str(const char *p, size_t n) { fwrite(p, 1, n, stdout); }

void zyxrt_print_i64(int64_t v) {
  char text[24];
  size_t n = zyxrt_fmt_i64(text, sizeof text, v);
  fwrite(text, 1, n, stdout);
}

void zyxrt_print_u64(uint64_t v) {
  char text[24];
  size_t n = zyxrt_fmt_u64(text, sizeof text, v);
  fwrite(text, 1, n, stdout);
}

void zyxrt_print_f64(double v) {
  char text[32];
  size_t n = zyxrt_fmt_f64(text, sizeof text, v);
  fwrite(text, 1, n, stdout);
}

void zyxrt_print_bool(int8_t v) {
  char text[8];
  size_t n = zyxrt_fmt_bool(text, sizeof text, v);
  fwrite(text, 1, n, stdout);
}

void zyxrt_print_char(uint32_t c) {
  char text[4];
  size_t n = zyxrt_fmt_char(text, sizeof text, c);
  fwrite(text, 1, n, stdout);
}

void zyxrt_print_nl(void) { putchar('\n'); }
