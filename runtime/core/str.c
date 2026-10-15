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

/* The text of one value is written into storage the region owns, and its
   address and length are reported through the out parameters. */

void zyxrt_i64_str(int64_t v, const char **out, size_t *len) {
    char *text = (char *)zyxrt_region_alloc_current(24, 1);
    int n = snprintf(text, 24, "%lld", (long long)v);
    *out = text;
    *len = n < 0 ? 0 : (size_t)n;
}

void zyxrt_u64_str(uint64_t v, const char **out, size_t *len) {
    char *text = (char *)zyxrt_region_alloc_current(24, 1);
    int n = snprintf(text, 24, "%llu", (unsigned long long)v);
    *out = text;
    *len = n < 0 ? 0 : (size_t)n;
}

void zyxrt_f64_str(double v, const char **out, size_t *len) {
    char *text = (char *)zyxrt_region_alloc_current(32, 1);
    int n = snprintf(text, 32, "%g", v);
    *out = text;
    *len = n < 0 ? 0 : (size_t)n;
}

void zyxrt_bool_str(int8_t v, const char **out, size_t *len) {
    if (v) {
        *out = "true";
        *len = 4;
    } else {
        *out = "false";
        *len = 5;
    }
}

/* zyxrt_char_str writes one Unicode scalar value as UTF-8, the same way the
   printing helper does. */
void zyxrt_char_str(uint32_t c, const char **out, size_t *len) {
    char *text = (char *)zyxrt_region_alloc_current(4, 1);
    size_t n = 0;
    if (c < 0x80) {
        text[n++] = (char)c;
    } else if (c < 0x800) {
        text[n++] = (char)(0xC0 | (c >> 6));
        text[n++] = (char)(0x80 | (c & 0x3F));
    } else if (c < 0x10000) {
        text[n++] = (char)(0xE0 | (c >> 12));
        text[n++] = (char)(0x80 | ((c >> 6) & 0x3F));
        text[n++] = (char)(0x80 | (c & 0x3F));
    } else {
        text[n++] = (char)(0xF0 | (c >> 18));
        text[n++] = (char)(0x80 | ((c >> 12) & 0x3F));
        text[n++] = (char)(0x80 | ((c >> 6) & 0x3F));
        text[n++] = (char)(0x80 | (c & 0x3F));
    }
    *out = text;
    *len = n;
}
