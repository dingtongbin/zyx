/* String operations that generated code calls instead of inlining. A str is
   a pointer and a length, so every operation takes both and never reads past
   either string.

   The formatting functions exist so an interpolated value becomes text the
   same way the printing helpers write it: the format of a number is one rule,
   and both users share it. */

#include "zyxrt.h"

#include <stdio.h>
#include <string.h>

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

/* The text of one value is appended to a buffer, in the same form the
   printing helpers write. The text is built on the stack and never becomes a
   string of its own, so a value costs one pass over its own bytes. */

void zyxrt_buf_write_i64(void *handle, int64_t v) {
    char text[24];
    int n = snprintf(text, sizeof text, "%lld", (long long)v);
    if (n > 0) {
        zyxrt_buf_write(handle, (const unsigned char *)text, (size_t)n);
    }
}

void zyxrt_buf_write_u64(void *handle, uint64_t v) {
    char text[24];
    int n = snprintf(text, sizeof text, "%llu", (unsigned long long)v);
    if (n > 0) {
        zyxrt_buf_write(handle, (const unsigned char *)text, (size_t)n);
    }
}

void zyxrt_buf_write_f64(void *handle, double v) {
    char text[32];
    int n = snprintf(text, sizeof text, "%g", v);
    if (n > 0) {
        zyxrt_buf_write(handle, (const unsigned char *)text, (size_t)n);
    }
}

void zyxrt_buf_write_bool(void *handle, int8_t v) {
    if (v) {
        zyxrt_buf_write(handle, (const unsigned char *)"true", 4);
    } else {
        zyxrt_buf_write(handle, (const unsigned char *)"false", 5);
    }
}

/* zyxrt_buf_write_char appends one Unicode scalar value as UTF-8, the same
   way the printing helper writes it. */
void zyxrt_buf_write_char(void *handle, uint32_t c) {
    unsigned char text[4];
    size_t n = 0;
    if (c < 0x80) {
        text[n++] = (unsigned char)c;
    } else if (c < 0x800) {
        text[n++] = (unsigned char)(0xC0 | (c >> 6));
        text[n++] = (unsigned char)(0x80 | (c & 0x3F));
    } else if (c < 0x10000) {
        text[n++] = (unsigned char)(0xE0 | (c >> 12));
        text[n++] = (unsigned char)(0x80 | ((c >> 6) & 0x3F));
        text[n++] = (unsigned char)(0x80 | (c & 0x3F));
    } else {
        text[n++] = (unsigned char)(0xF0 | (c >> 18));
        text[n++] = (unsigned char)(0x80 | ((c >> 12) & 0x3F));
        text[n++] = (unsigned char)(0x80 | ((c >> 6) & 0x3F));
        text[n++] = (unsigned char)(0x80 | (c & 0x3F));
    }
    zyxrt_buf_write(handle, text, n);
}
