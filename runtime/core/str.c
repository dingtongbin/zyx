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

/* zyxrt_str_compare orders two strings by their bytes, which is the order a
   sorted container keeps them in. It answers a negative number, zero, or a
   positive number. */
int32_t zyxrt_str_compare(const char *a, size_t an, const char *b, size_t bn) {
    size_t n = an < bn ? an : bn;
    for (size_t i = 0; i < n; i++) {
        unsigned char x = (unsigned char)a[i];
        unsigned char y = (unsigned char)b[i];
        if (x != y) {
            return x < y ? -1 : 1;
        }
    }
    if (an == bn) {
        return 0;
    }
    return an < bn ? -1 : 1;
}

/* zyxrt_str_rune decodes the rune at a byte offset and answers where the next
   one begins. A byte that starts no valid sequence, a sequence cut short by the
   end of the string, an overlong form, a surrogate, and a value above the
   Unicode range are all U+FFFD, and the offset then advances by one byte: a
   decoder that refused the input would leave the caller nowhere to go. */
uint32_t zyxrt_str_rune(const char *s, size_t len, size_t at, size_t *next) {
    static const uint32_t least[5] = {0, 0, 0x80, 0x800, 0x10000};
    if (at >= len) {
        if (next != NULL) *next = at;
        return 0xFFFD;
    }
    unsigned char first = (unsigned char)s[at];
    size_t size = 0;
    uint32_t rune = 0;
    if (first < 0x80) {
        size = 1;
        rune = first;
    } else if ((first & 0xE0) == 0xC0) {
        size = 2;
        rune = first & 0x1Fu;
    } else if ((first & 0xF0) == 0xE0) {
        size = 3;
        rune = first & 0x0Fu;
    } else if ((first & 0xF8) == 0xF0) {
        size = 4;
        rune = first & 0x07u;
    } else {
        if (next != NULL) *next = at + 1;
        return 0xFFFD;
    }
    if (at + size > len) {
        if (next != NULL) *next = at + 1;
        return 0xFFFD;
    }
    for (size_t i = 1; i < size; i++) {
        unsigned char b = (unsigned char)s[at + i];
        if ((b & 0xC0) != 0x80) {
            if (next != NULL) *next = at + 1;
            return 0xFFFD;
        }
        rune = (rune << 6) | (uint32_t)(b & 0x3Fu);
    }
    if (rune < least[size] || rune > 0x10FFFFu || (rune >= 0xD800u && rune <= 0xDFFFu)) {
        if (next != NULL) *next = at + 1;
        return 0xFFFD;
    }
    if (next != NULL) *next = at + size;
    return rune;
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
