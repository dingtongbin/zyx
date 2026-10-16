/* Buf: a mutable byte buffer. The storage belongs to the region that created
   the buffer, so a buffer that grows leaves its earlier storage behind to be
   released with the region.

   Bytes() answers with the address and the length of the storage, which is
   what a view of the buffer is: the caller reads the bytes in place rather
   than through a copy. */

#include "zyxrt.h"
#include "zyxrt_internal.h"

#include <string.h>

#define ZYXXRT_BUF_MIN 16u

typedef struct zyxrt_buf {
  size_t len;
  size_t cap;
  /* start is the first live byte, which Consume advances. */
  size_t start;
  unsigned char *data;
} zyxrt_buf;

void *zyxrt_buf_new(size_t cap) {
  zyxrt_buf *b = (zyxrt_buf *)zyxrt_region_alloc_current(sizeof(zyxrt_buf), 8);
  b->len = 0;
  b->start = 0;
  b->cap = cap < ZYXXRT_BUF_MIN ? ZYXXRT_BUF_MIN : cap;
  b->data = (unsigned char *)zyxrt_region_alloc_current(b->cap, 8);
  return b;
}

size_t zyxrt_buf_len(void *handle) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  return b == NULL ? 0 : b->len - b->start;
}

size_t zyxrt_buf_cap(void *handle) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  return b == NULL ? 0 : b->cap;
}

void zyxrt_buf_clear(void *handle) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  if (b == NULL) return;
  b->len = 0;
  b->start = 0;
}

/* zyxrt_buf_reserve makes room for n more bytes. The live bytes are moved to
   the front when the space before them is what stands in the way. */
void zyxrt_buf_reserve(void *handle, size_t n) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  if (b == NULL) return;
  size_t live = b->len - b->start;
  if (b->start + live + n <= b->cap) {
    if (b->start > 0 && b->start + live + n > b->cap - 0) {
      /* The room is at the front, so the live bytes move there. */
      memmove(b->data, b->data + b->start, live);
      b->len = live;
      b->start = 0;
    }
    return;
  }
  if (b->start > 0) {
    memmove(b->data, b->data + b->start, live);
    b->len = live;
    b->start = 0;
  }
  size_t cap = b->cap;
  while (cap < b->len + n) {
    cap *= 2;
  }
  unsigned char *grown = (unsigned char *)zyxrt_region_alloc_current(cap, 8);
  memcpy(grown, b->data, b->len);
  b->data = grown;
  b->cap = cap;
}

void zyxrt_buf_write(void *handle, const unsigned char *p, size_t n) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  if (b == NULL || n == 0) return;
  zyxrt_buf_reserve(handle, n);
  memcpy(b->data + b->len, p, n);
  b->len += n;
}

void zyxrt_buf_write_byte(void *handle, unsigned char value) {
  zyxrt_buf_write(handle, &value, 1);
}

/* zyxrt_buf_consume drops n bytes from the front. It is O(1) because the
   storage is not moved until room is needed. */
void zyxrt_buf_consume(void *handle, size_t n) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  if (b == NULL) return;
  size_t live = b->len - b->start;
  b->start += n > live ? live : n;
}

/* zyxrt_buf_bytes reports the live bytes in place, which is the view a caller
   reads. */
void zyxrt_buf_bytes(void *handle, const unsigned char **out, size_t *len) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  if (b == NULL) {
    *out = (const unsigned char *)"";
    *len = 0;
    return;
  }
  *out = b->data + b->start;
  *len = b->len - b->start;
}

/* zyxrt_buf_str copies the live bytes into a text of their own, so the text
   outlives the buffer it was read from. */
void zyxrt_buf_str(void *handle, const char **out, size_t *len) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  size_t live = b == NULL ? 0 : b->len - b->start;
  char *text = (char *)zyxrt_region_alloc_current(live + 1, 1);
  if (live > 0) {
    memcpy(text, b->data + b->start, live);
  }
  text[live] = '\0';
  *out = text;
  *len = live;
}

/* zyxrt_buf_take hands the live bytes to an array and empties the buffer. */
void *zyxrt_buf_take(void *handle) {
  zyxrt_buf *b = (zyxrt_buf *)handle;
  size_t live = b == NULL ? 0 : b->len - b->start;
  void *array = zyxrt_array_new(1, live);
  if (live > 0) {
    zyxrt_array_append(array, b->data + b->start, live);
    zyxrt_buf_clear(handle);
  }
  return array;
}
