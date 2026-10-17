/* The zstd binding of the compress package: the library is asked for a frame's
   bytes and the answer is handed to the runtime's buffer, so a program keeps
   reading and writing one buffer type wherever the bytes came from. */

#include "zyxcompress.h"

#include <stdlib.h>

#include <zstd.h>

#include "zyxrt.h"

int32_t zyxcompress_compress(void *dst, const unsigned char *src, size_t n, int32_t level) {
  if (level < ZSTD_minCLevel() || level > ZSTD_maxCLevel()) {
    return 1;
  }
  size_t bound = ZSTD_compressBound(n);
  unsigned char *out = (unsigned char *)malloc(bound == 0 ? 1 : bound);
  if (out == NULL) {
    return 2;
  }
  size_t wrote = ZSTD_compress(out, bound, src, n, level);
  if (ZSTD_isError(wrote)) {
    free(out);
    return 2;
  }
  zyxrt_buf_write(dst, out, wrote);
  free(out);
  return 0;
}

int32_t zyxcompress_decompress(void *dst, const unsigned char *src, size_t n) {
  unsigned long long size = ZSTD_getFrameContentSize(src, n);
  if (size == ZSTD_CONTENTSIZE_ERROR) {
    return 1;
  }
  if (size == ZSTD_CONTENTSIZE_UNKNOWN) {
    return 2;
  }
  /* A frame of nothing is a frame with an empty body, which the library writes
     and reads without a buffer of its own. */
  unsigned char *out = (unsigned char *)malloc(size == 0 ? 1 : (size_t)size);
  if (out == NULL) {
    return 2;
  }
  size_t wrote = ZSTD_decompress(out, (size_t)size, src, n);
  if (ZSTD_isError(wrote)) {
    free(out);
    return 2;
  }
  zyxrt_buf_write(dst, out, wrote);
  free(out);
  return 0;
}
