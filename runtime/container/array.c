/* Array: a dynamic array of fixed-size elements. The buffer belongs to the
   region that created the array, so an array that grows leaves its earlier
   buffers behind to be released with the region, and no element is copied
   twice. */

#include "zyxrt.h"

#include <string.h>

/* The buffer an array grows from, and the array itself. */
#define ZYXXRT_ARRAY_MIN 4u

typedef struct zyxrt_array {
  size_t len;
  size_t cap;
  size_t elem;
  unsigned char *data;
} zyxrt_array;

void *zyxrt_array_new(size_t elem, size_t cap) {
  zyxrt_array *a = (zyxrt_array *)zyxrt_region_alloc_current(sizeof(zyxrt_array), 8);
  if (cap < ZYXXRT_ARRAY_MIN) cap = ZYXXRT_ARRAY_MIN;
  a->len = 0;
  a->cap = cap;
  a->elem = elem == 0 ? 1 : elem;
  a->data = (unsigned char *)zyxrt_region_alloc_current(a->cap * a->elem, 8);
  return a;
}

/* zyxrt_array_data returns the buffer, which the runtime writes into when it
   fills an array in one step. */
unsigned char *zyxrt_array_data(void *handle) {
  return ((zyxrt_array *)handle)->data;
}

size_t zyxrt_array_len(void *handle) {
  zyxrt_array *a = (zyxrt_array *)handle;
  return a == NULL ? 0 : a->len;
}

/* grow doubles the buffer. The elements are copied, and the buffer that is
   left behind is released with the region. */
static void grow(zyxrt_array *a) {
  size_t cap = a->cap * 2;
  unsigned char *grown = (unsigned char *)zyxrt_region_alloc_current(cap * a->elem, 8);
  memcpy(grown, a->data, a->len * a->elem);
  a->data = grown;
  a->cap = cap;
}

void zyxrt_array_push(void *handle, const void *value) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  if (a->len == a->cap) grow(a);
  memcpy(a->data + a->len * a->elem, value, a->elem);
  a->len++;
}

int zyxrt_array_pop(void *handle, void *out) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || a->len == 0) return 0;
  a->len--;
  memcpy(out, a->data + a->len * a->elem, a->elem);
  return 1;
}

int zyxrt_array_get(void *handle, size_t index, void *out) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || index >= a->len) return 0;
  memcpy(out, a->data + index * a->elem, a->elem);
  return 1;
}

void zyxrt_array_get_unchecked(void *handle, size_t index, void *out) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  memcpy(out, a->data + index * a->elem, a->elem);
}

void zyxrt_array_remove(void *handle, size_t index) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || index >= a->len) return;
  a->len--;
  memmove(a->data + index * a->elem, a->data + (index + 1) * a->elem,
          (a->len - index) * a->elem);
}
