/* String operations that generated code calls instead of inlining. A str is
   a pointer and a length, so every operation takes both and never reads past
   either string. */

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
