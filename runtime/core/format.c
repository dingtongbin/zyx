/* The text of one value, in the single form the language writes it. Each
   function renders into a buffer the caller owns and reports the byte count,
   so the printing helpers and the buffer writers share one rule and can never
   disagree about how a value reads.

   The integers are converted by hand, so their text depends on nothing
   outside this file. A floating-point value uses the C library's shortest
   form, which is the rule the language documents. */

#include "zyxrt.h"

#include <stdio.h>

size_t zyxrt_fmt_u64(char *out, size_t cap, uint64_t v) {
    char digits[20];
    size_t n = 0;
    do {
        digits[n++] = (char)('0' + (v % 10));
        v /= 10;
    } while (v != 0);
    if (n > cap) {
        n = cap;
    }
    for (size_t i = 0; i < n; i++) {
        out[i] = digits[n - 1 - i];
    }
    return n;
}

/* zyxrt_fmt_i64 writes the decimal form of a signed value, sign included. The
   magnitude of the smallest value does not fit in the signed type, so it is
   taken in the unsigned type. */
size_t zyxrt_fmt_i64(char *out, size_t cap, int64_t v) {
    if (v >= 0) {
        return zyxrt_fmt_u64(out, cap, (uint64_t)v);
    }
    if (cap == 0) {
        return 0;
    }
    out[0] = '-';
    return 1 + zyxrt_fmt_u64(out + 1, cap - 1, (uint64_t)(-(v + 1)) + 1);
}

size_t zyxrt_fmt_f64(char *out, size_t cap, double v) {
    if (cap == 0) {
        return 0;
    }
    int n = snprintf(out, cap, "%g", v);
    if (n <= 0) {
        return 0;
    }
    return (size_t)n < cap ? (size_t)n : cap - 1;
}

size_t zyxrt_fmt_bool(char *out, size_t cap, int8_t v) {
    const char *text = v ? "true" : "false";
    size_t n = v ? 4 : 5;
    if (n > cap) {
        n = cap;
    }
    for (size_t i = 0; i < n; i++) {
        out[i] = text[i];
    }
    return n;
}

/* zyxrt_fmt_char writes one Unicode scalar value as UTF-8. */
size_t zyxrt_fmt_char(char *out, size_t cap, uint32_t c) {
    char text[4];
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
    if (n > cap) {
        n = cap;
    }
    for (size_t i = 0; i < n; i++) {
        out[i] = text[i];
    }
    return n;
}
