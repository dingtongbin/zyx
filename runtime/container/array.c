/* Array: a dynamic array of fixed-size elements. The buffer belongs to the
   region that created the array, so an array that grows leaves its earlier
   buffers behind to be released with the region, and no element is copied
   twice. */

#include "zyxrt.h"

#include <string.h>

/* The buffer an array grows from, and the array itself. */
#define ZYXXRT_ARRAY_MIN 4u
/* The bytes one exchange moves at a time, which keeps an exchange free of
   allocation whatever the element size is. */
#define ZYXXRT_ARRAY_SWAP_CHUNK 32u

typedef struct zyxrt_array {
  size_t len;
  size_t cap;
  size_t elem;
  /* mod_gen counts the structural modifications, which is what an iteration
     compares to notice that the buffer it walks is no longer the one the
     container holds. */
  size_t mod_gen;
  unsigned char *data;
} zyxrt_array;

void *zyxrt_array_new(size_t elem, size_t cap) {
  zyxrt_array *a = (zyxrt_array *)zyxrt_region_alloc_current(sizeof(zyxrt_array), 8);
  if (cap < ZYXXRT_ARRAY_MIN) cap = ZYXXRT_ARRAY_MIN;
  a->len = 0;
  a->cap = cap;
  a->elem = elem == 0 ? 1 : elem;
  a->mod_gen = 0;
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

/* touch records a structural modification, which an iteration compares against
   the count it took when it began. */
static void touch(zyxrt_array *a) { a->mod_gen++; }

/* zyxrt_array_mod_gen answers the structural modification count, which a loop
   compares each round: a difference means the container it walks was modified
   and the language makes that a fault. */
size_t zyxrt_array_mod_gen(void *handle) {
  zyxrt_array *a = (zyxrt_array *)handle;
  return a == NULL ? 0 : a->mod_gen;
}

/* zyxrt_array_set_len records that n elements are live. It is what a caller
   uses when it wrote them through zyxrt_array_data, so an array filled in one
   step reports the length of what it holds. */
void zyxrt_array_set_len(void *handle, size_t n) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  a->len = n;
  touch(a);
}

/* zyxrt_array_append adds n elements in one step, which the runtime uses when
   it already has the bytes. */
void zyxrt_array_append(void *handle, const void *data, size_t n) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || n == 0) return;
  while (a->len + n > a->cap) {
    grow(a);
  }
  memcpy(a->data + a->len * a->elem, data, n * a->elem);
  a->len += n;
  touch(a);
}

void zyxrt_array_push(void *handle, const void *value) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  if (a->len == a->cap) grow(a);
  memcpy(a->data + a->len * a->elem, value, a->elem);
  a->len++;
  touch(a);
}

int zyxrt_array_pop(void *handle, void *out) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || a->len == 0) return 0;
  a->len--;
  memcpy(out, a->data + a->len * a->elem, a->elem);
  touch(a);
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

/* zyxrt_array_slot returns the address of an element, which the compiler
   writes through when an element or one of its fields is assigned. The address
   stays valid until the array is structurally modified, because that may move
   the buffer. */
void *zyxrt_array_slot(void *handle, size_t index) {
  zyxrt_array *a = (zyxrt_array *)handle;
  return a->data + index * a->elem;
}

/* zyxrt_array_swap exchanges two elements. The exchange happens in the
   buffer, so it serves an element of any size without a temporary the caller
   would have to own: an element of a reference type names storage inside the
   sequence, and a temporary would keep naming it after the exchange moved
   it. */
void zyxrt_array_swap(void *handle, size_t a, size_t b) {
  zyxrt_array *array = (zyxrt_array *)handle;
  if (array == NULL) return;
  if (a >= array->len || b >= array->len) {
    zyxrt_panic("index out of range", 18);
  }
  if (a == b) return;
  unsigned char *x = array->data + a * array->elem;
  unsigned char *y = array->data + b * array->elem;
  unsigned char held[ZYXXRT_ARRAY_SWAP_CHUNK];
  size_t left = array->elem;
  while (left > 0) {
    size_t chunk = left < ZYXXRT_ARRAY_SWAP_CHUNK ? left : ZYXXRT_ARRAY_SWAP_CHUNK;
    memcpy(held, x, chunk);
    memcpy(x, y, chunk);
    memcpy(y, held, chunk);
    x += chunk;
    y += chunk;
    left -= chunk;
  }
}

/* zyxrt_array_remove drops one element, moving what follows it. */
void zyxrt_array_remove(void *handle, size_t index) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL || index >= a->len) return;
  a->len--;
  memmove(a->data + index * a->elem, a->data + (index + 1) * a->elem,
          (a->len - index) * a->elem);
  touch(a);
}

size_t zyxrt_array_cap(void *handle) {
  zyxrt_array *a = (zyxrt_array *)handle;
  return a == NULL ? 0 : a->cap;
}

/* zyxrt_array_clear drops every element and keeps the buffer, so an array
   that is filled again does not allocate again. */
void zyxrt_array_clear(void *handle) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  a->len = 0;
  touch(a);
}

/* zyxrt_array_reserve makes room for n elements, which is what a caller does
   when it knows how many are coming. */
void zyxrt_array_reserve(void *handle, size_t n) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  if (a->cap >= n) return;
  while (a->cap < n) {
    grow(a);
  }
  touch(a);
}

/* zyxrt_array_insert puts one element at an index, moving what follows it. An
   index past the end is a fault rather than a silent append: the caller's
   check is what makes this call, and a library that accepts anything would
   hide the mistake. */
void zyxrt_array_insert(void *handle, size_t index, const void *value) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  if (index > a->len) zyxrt_panic("index out of range", 18);
  if (a->len == a->cap) grow(a);
  memmove(a->data + (index + 1) * a->elem, a->data + index * a->elem,
          (a->len - index) * a->elem);
  memcpy(a->data + index * a->elem, value, a->elem);
  a->len++;
  touch(a);
}

/* zyxrt_array_remove_range drops n elements from an index, moving what
   follows them. A range that reaches past the end is a fault. */
void zyxrt_array_remove_range(void *handle, size_t index, size_t n) {
  zyxrt_array *a = (zyxrt_array *)handle;
  if (a == NULL) return;
  if (index > a->len || n > a->len - index) zyxrt_panic("index out of range", 18);
  memmove(a->data + index * a->elem, a->data + (index + n) * a->elem,
          (a->len - index - n) * a->elem);
  a->len -= n;
  touch(a);
}
