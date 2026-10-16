/* Map: a hash map from fixed-size keys to fixed-size values. The table
   belongs to the region that created the map, so growth leaves the earlier
   bucket arrays behind to be released with the region.

   A key is hashed and compared through two functions the caller supplies, so
   the same table serves a scalar key, a string key, and a key of a type with
   its own Hash and Eq. */

#include "zyxrt.h"
#include "zyxrt_internal.h"

#include <string.h>

/* The buckets a map starts with, and the load factor at which it grows. */
#define ZYXXRT_MAP_MIN 8u

typedef uint64_t (*zyxrt_hash_fn)(const void *key, size_t size);
typedef int (*zyxrt_eq_fn)(const void *a, const void *b, size_t size);

typedef struct zyxrt_map_entry {
  struct zyxrt_map_entry *next;
  unsigned char *key;
  unsigned char *value;
} zyxrt_map_entry;

typedef struct zyxrt_map {
  size_t len;
  size_t cap;
  size_t key_size;
  size_t val_size;
  zyxrt_hash_fn hash;
  zyxrt_eq_fn eq;
  zyxrt_map_entry **buckets;
} zyxrt_map;

/* hash_bytes is the fallback hash: it reads the bytes of the key, which is
   what a scalar key needs. A key whose equality is not its bytes, such as a
   string, is given its own hash by the caller. */
static uint64_t hash_bytes(const void *key, size_t size) {
  const unsigned char *p = (const unsigned char *)key;
  uint64_t h = 1469598103934665603u;
  for (size_t i = 0; i < size; i++) {
    h ^= p[i];
    h *= 1099511628211u;
  }
  return h;
}

static zyxrt_map_entry **buckets_for(size_t cap) {
  return (zyxrt_map_entry **)zyxrt_region_alloc_current(cap * sizeof(zyxrt_map_entry *), 8);
}

void *zyxrt_map_new(size_t key_size, size_t val_size, void *hash, void *eq) {
  zyxrt_map *m = (zyxrt_map *)zyxrt_region_alloc_current(sizeof(zyxrt_map), 8);
  m->len = 0;
  m->cap = ZYXXRT_MAP_MIN;
  m->key_size = key_size == 0 ? 1 : key_size;
  m->val_size = val_size == 0 ? 1 : val_size;
  m->hash = (zyxrt_hash_fn)hash;
  m->eq = (zyxrt_eq_fn)eq;
  m->buckets = buckets_for(m->cap);
  memset(m->buckets, 0, m->cap * sizeof(zyxrt_map_entry *));
  return m;
}

size_t zyxrt_map_len(void *handle) {
  zyxrt_map *m = (zyxrt_map *)handle;
  return m == NULL ? 0 : m->len;
}

/* find returns the entry of a key, or NULL. */
static zyxrt_map_entry *find(zyxrt_map *m, const void *key) {
  size_t index = (size_t)(m->hash(key, m->key_size) & (m->cap - 1));
  for (zyxrt_map_entry *e = m->buckets[index]; e != NULL; e = e->next) {
    if (m->eq(e->key, key, m->key_size)) {
      return e;
    }
  }
  return NULL;
}

/* rehash doubles the bucket array and moves every entry into it. */
static void rehash(zyxrt_map *m) {
  size_t cap = m->cap * 2;
  zyxrt_map_entry **grown = buckets_for(cap);
  memset(grown, 0, cap * sizeof(zyxrt_map_entry *));
  for (size_t i = 0; i < m->cap; i++) {
    zyxrt_map_entry *e = m->buckets[i];
    while (e != NULL) {
      zyxrt_map_entry *next = e->next;
      size_t index = (size_t)(m->hash(e->key, m->key_size) & (cap - 1));
      e->next = grown[index];
      grown[index] = e;
      e = next;
    }
  }
  m->buckets = grown;
  m->cap = cap;
}

void zyxrt_map_set(void *handle, const void *key, const void *value) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return;
  if (m->len + 1 > m->cap) {
    rehash(m);
  }
  zyxrt_map_entry *e = find(m, key);
  if (e == NULL) {
    e = (zyxrt_map_entry *)zyxrt_region_alloc_current(sizeof(zyxrt_map_entry), 8);
    e->key = (unsigned char *)zyxrt_region_alloc_current(m->key_size, 8);
    e->value = (unsigned char *)zyxrt_region_alloc_current(m->val_size, 8);
    memcpy(e->key, key, m->key_size);
    size_t index = (size_t)(m->hash(e->key, m->key_size) & (m->cap - 1));
    e->next = m->buckets[index];
    m->buckets[index] = e;
    m->len++;
  }
  memcpy(e->value, value, m->val_size);
}

int zyxrt_map_get(void *handle, const void *key, void *out) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return 0;
  zyxrt_map_entry *e = find(m, key);
  if (e == NULL) return 0;
  memcpy(out, e->value, m->val_size);
  return 1;
}

int zyxrt_map_contains(void *handle, const void *key) {
  zyxrt_map *m = (zyxrt_map *)handle;
  return m != NULL && find(m, key) != NULL;
}

int zyxrt_map_remove(void *handle, const void *key, void *out) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return 0;
  size_t index = (size_t)(m->hash(key, m->key_size) & (m->cap - 1));
  zyxrt_map_entry **link = &m->buckets[index];
  while (*link != NULL) {
    zyxrt_map_entry *e = *link;
    if (m->eq(e->key, key, m->key_size)) {
      if (out != NULL) {
        memcpy(out, e->value, m->val_size);
      }
      *link = e->next;
      m->len--;
      return 1;
    }
    link = &e->next;
  }
  return 0;
}

/* zyxrt_map_keys copies every key into a buffer the caller made room for, in
   the order the buckets hold them. */
void zyxrt_map_keys(void *handle, void *out) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return;
  unsigned char *p = (unsigned char *)out;
  for (size_t i = 0; i < m->cap; i++) {
    for (zyxrt_map_entry *e = m->buckets[i]; e != NULL; e = e->next) {
      memcpy(p, e->key, m->key_size);
      p += m->key_size;
    }
  }
}

/* zyxrt_map_values copies every value into a buffer the caller made room for,
   in the same order as the keys. */
void zyxrt_map_values(void *handle, void *out) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return;
  unsigned char *p = (unsigned char *)out;
  for (size_t i = 0; i < m->cap; i++) {
    for (zyxrt_map_entry *e = m->buckets[i]; e != NULL; e = e->next) {
      memcpy(p, e->value, m->val_size);
      p += m->val_size;
    }
  }
}

/* The hashes and comparisons the language uses for the keys it knows. A
   scalar key is hashed by its bytes and compared by them, which the caller
   asks for by passing zero, so only the keys whose equality is not their
   bytes need a function of their own. */

uint64_t zyxrt_hash_bytes(const void *key, size_t size) {
  return hash_bytes(key, size);
}

int zyxrt_eq_bytes(const void *a, const void *b, size_t size) {
  return memcmp(a, b, size) == 0;
}

/* A string key is a fat pointer, so its hash and its comparison read the
   bytes the pointer names rather than the pointer itself. */
uint64_t zyxrt_hash_str_key(const void *key, size_t size) {
  (void)size;
  const char *p = *(const char *const *)key;
  size_t n = *((const size_t *)((const unsigned char *)key + sizeof(void *)));
  return hash_bytes(p, n);
}

int zyxrt_eq_str_key(const void *a, const void *b, size_t size) {
  (void)size;
  const char *ap = *(const char *const *)a;
  size_t an = *((const size_t *)((const unsigned char *)a + sizeof(void *)));
  const char *bp = *(const char *const *)b;
  size_t bn = *((const size_t *)((const unsigned char *)b + sizeof(void *)));
  return zyxrt_str_eq(ap, an, bp, bn);
}

/* zyxrt_map_key_list builds an Array that holds every key, which is what
   KeyList answers with. */
void *zyxrt_map_key_list(void *handle, size_t key_size) {
  zyxrt_map *m = (zyxrt_map *)handle;
  void *list = zyxrt_array_new(key_size, m == NULL ? 0 : m->len);
  if (m != NULL && m->len > 0) {
    zyxrt_map_keys(m, zyxrt_array_data(list));
  }
  return list;
}

/* A set is a map whose value is one byte the caller never reads, so the key
   is the element and every operation the map has serves the set. */

void *zyxrt_set_new(size_t elem_size, void *hash, void *eq) {
  return zyxrt_map_new(elem_size, 1, hash, eq);
}

size_t zyxrt_set_len(void *handle) { return zyxrt_map_len(handle); }

void zyxrt_set_add(void *handle, const void *elem) {
  unsigned char dummy = 0;
  zyxrt_map_set(handle, elem, &dummy);
}

int zyxrt_set_contains(void *handle, const void *elem) {
  return zyxrt_map_contains(handle, elem);
}

/* zyxrt_set_remove takes an element out and copies the element itself into
   out, which is what the language's Remove answers with. */
int zyxrt_set_remove(void *handle, const void *elem, void *out) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return 0;
  size_t index = (size_t)(m->hash(elem, m->key_size) & (m->cap - 1));
  zyxrt_map_entry **link = &m->buckets[index];
  while (*link != NULL) {
    zyxrt_map_entry *e = *link;
    if (m->eq(e->key, elem, m->key_size)) {
      if (out != NULL) {
        memcpy(out, e->key, m->key_size);
      }
      *link = e->next;
      m->len--;
      return 1;
    }
    link = &e->next;
  }
  return 0;
}
