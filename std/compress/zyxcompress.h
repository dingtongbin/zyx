/* The zstd binding of the compress package. zstd is a C library with no
   counterpart in the language, so the package reaches it through this surface:
   the two entries the design names, and the code each failure carries. */

#ifndef ZYXCOMPRESS_H
#define ZYXCOMPRESS_H

#include <stddef.h>
#include <stdint.h>

/* zyxcompress_compress appends the zstd frame of src to dst, at a level between
   one and twenty-two, and answers 0. It answers 1 when the level is outside the
   range the library takes, and 2 when the library failed. */
int32_t zyxcompress_compress(void *dst, const unsigned char *src, size_t n, int32_t level);

/* zyxcompress_decompress appends the bytes a zstd frame stands for to dst and
   answers 0. It answers 1 when the bytes are not a zstd frame, and 2 when the
   frame is one whose content size it does not declare, which is a frame this
   package cannot size a buffer for. */
int32_t zyxcompress_decompress(void *dst, const unsigned char *src, size_t n);

#endif
