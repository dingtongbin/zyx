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

/* divmod10 divides the 128-bit value (hi, lo) by ten in place and returns the
   remainder. The quotient of the low half needs the remainder carried in from
   the high half, so it is computed from 2^64 = 10 * Q + R: the high remainder
   contributes Q per unit and the low half is divided with that carry added,
   which never overflows. */
static uint64_t divmod10(uint64_t *hi, uint64_t *lo) {
    const uint64_t q = 1844674407370955161ULL; /* floor(2^64 / 10) */
    uint64_t h = *hi;
    uint64_t l = *lo;
    uint64_t rem = h % 10;
    /* rem * 2^64 + l = 10 * (rem * q) + (rem * 6 + l). */
    uint64_t t = l + rem * 6;
    uint64_t carry = t < l ? 1 : 0;
    /* The carry is one at most, and it can only be set when l is within 54 of
       the top, so t + 6 cannot wrap. */
    uint64_t rest = carry * 6 + t;
    *hi = h / 10;
    *lo = (rem + carry) * q + rest / 10;
    return rest % 10;
}

size_t zyxrt_fmt_u128(char *out, size_t cap, uint64_t hi, uint64_t lo) {
    char digits[40];
    size_t n = 0;
    do {
        digits[n++] = (char)('0' + divmod10(&hi, &lo));
    } while (hi != 0 || lo != 0);
    if (n > cap) {
        n = cap;
    }
    for (size_t i = 0; i < n; i++) {
        out[i] = digits[n - 1 - i];
    }
    return n;
}

/* zyxrt_fmt_i128 writes the decimal form of a signed 128-bit value, sign
   included. The magnitude of the smallest value does not fit in the signed
   type, so it is taken in the unsigned one, which is the two's complement of
   the whole pair. */
size_t zyxrt_fmt_i128(char *out, size_t cap, int64_t hi, uint64_t lo) {
    if (hi >= 0) {
        return zyxrt_fmt_u128(out, cap, (uint64_t)hi, lo);
    }
    if (cap == 0) {
        return 0;
    }
    out[0] = '-';
    uint64_t magnitude_hi = ~(uint64_t)hi + (lo == 0 ? 1 : 0);
    uint64_t magnitude_lo = ~lo + 1;
    return 1 + zyxrt_fmt_u128(out + 1, cap - 1, magnitude_hi, magnitude_lo);
}
