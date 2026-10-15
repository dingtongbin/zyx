/* String operations that generated code calls instead of inlining. A str is
   a pointer and a length, so every operation takes both and never reads past
   either string.

   A value is appended to a buffer by rendering it with the shared formatter,
   so it reads exactly as the printing helper writes it. */

#include "zyxrt.h"

int8_t zyxrt_str_eq(const char *a, size_t an, const char *b, size_t bn) {
    if (an != bn) {
        return 0;
    }
    for (size_t i = 0; i < an; i++) {
        if ((unsigned char)a[i] != (unsigned char)b[i]) {
            return 0;
        }
    }
    return 1;
}

/* The text of one value is appended to a buffer, rendered by the shared
   formatter so it reads exactly as the printing helper writes it. */

void zyxrt_buf_write_i64(void *handle, int64_t v) {
    char text[24];
    size_t n = zyxrt_fmt_i64(text, sizeof text, v);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_u64(void *handle, uint64_t v) {
    char text[24];
    size_t n = zyxrt_fmt_u64(text, sizeof text, v);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_f64(void *handle, double v) {
    char text[32];
    size_t n = zyxrt_fmt_f64(text, sizeof text, v);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_bool(void *handle, int8_t v) {
    char text[8];
    size_t n = zyxrt_fmt_bool(text, sizeof text, v);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_char(void *handle, uint32_t c) {
    char text[4];
    size_t n = zyxrt_fmt_char(text, sizeof text, c);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

/* zyxrt_buf_write_dur writes a duration as its nanosecond count. A Duration
   is a count of nanoseconds and nothing else, so its text is that count, and
   it has an entry of its own so a unit-aware form would replace one function
   rather than change how an unsigned value reads. */
void zyxrt_buf_write_dur(void *handle, uint64_t ns) {
    char text[24];
    size_t n = zyxrt_fmt_u64(text, sizeof text, ns);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_i128(void *handle, int64_t hi, uint64_t lo) {
    char text[48];
    size_t n = zyxrt_fmt_i128(text, sizeof text, hi, lo);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}

void zyxrt_buf_write_u128(void *handle, uint64_t hi, uint64_t lo) {
    char text[48];
    size_t n = zyxrt_fmt_u128(text, sizeof text, hi, lo);
    zyxrt_buf_write(handle, (const unsigned char *)text, n);
}
