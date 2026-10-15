/* C ABI helpers shared by generated code: the output helpers the standard
   library calls, so a str crosses the boundary as a borrowed pointer and
   its length rather than as a NUL-terminated buffer. */

#include "zyxrt.h"

#include <stdio.h>

void zyxrt_print_str(const char *p, size_t n) { fwrite(p, 1, n, stdout); }

void zyxrt_print_i64(int64_t v) { printf("%lld", (long long)v); }

void zyxrt_print_u64(uint64_t v) { printf("%llu", (unsigned long long)v); }

void zyxrt_print_f64(double v) { printf("%g", v); }

void zyxrt_print_bool(int8_t v) { fputs(v ? "true" : "false", stdout); }

/* zyxrt_print_char writes one Unicode scalar value as UTF-8. */
void zyxrt_print_char(uint32_t c) {
  if (c < 0x80) {
    putchar((int)c);
    return;
  }
  if (c < 0x800) {
    putchar((int)(0xC0 | (c >> 6)));
    putchar((int)(0x80 | (c & 0x3F)));
    return;
  }
  if (c < 0x10000) {
    putchar((int)(0xE0 | (c >> 12)));
    putchar((int)(0x80 | ((c >> 6) & 0x3F)));
    putchar((int)(0x80 | (c & 0x3F)));
    return;
  }
  putchar((int)(0xF0 | (c >> 18)));
  putchar((int)(0x80 | ((c >> 12) & 0x3F)));
  putchar((int)(0x80 | ((c >> 6) & 0x3F)));
  putchar((int)(0x80 | (c & 0x3F)));
}

void zyxrt_print_nl(void) { putchar('\n'); }
