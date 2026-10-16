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
  /* mod_gen counts the structural modifications, which is what an iteration
     compares to notice that the table it walks is no longer the one the map
     holds. */
  size_t mod_gen;
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
  m->mod_gen = 0;
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
    /* Adding an entry changes what an iteration walks, and a rehash moves
       every entry it already had. Overwriting a value changes neither. */
    m->mod_gen++;
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

/* zyxrt_map_slot returns the address of the value of a key, or NULL when the
   key is not there. An entry is allocated once and its value is written in
   place, so the address stays valid until the entry is removed. */
void *zyxrt_map_slot(void *handle, const void *key) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return NULL;
  zyxrt_map_entry *e = find(m, key);
  return e == NULL ? NULL : e->value;
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
      m->mod_gen++;
      return 1;
    }
    link = &e->next;
  }
  return 0;
}

/* zyxrt_map_mod_gen answers the structural modification count, which a loop
   compares each round: a difference means the map it walks was modified and
   the language makes that a fault. */
size_t zyxrt_map_mod_gen(void *handle) {
  zyxrt_map *m = (zyxrt_map *)handle;
  return m == NULL ? 0 : m->mod_gen;
}

/* zyxrt_map_clear drops every entry and keeps the table, so a map filled again
   does not allocate again. */
void zyxrt_map_clear(void *handle) {
  zyxrt_map *m = (zyxrt_map *)handle;
  if (m == NULL) return;
  for (size_t i = 0; i < m->cap; i++) {
    m->buckets[i] = NULL;
  }
  m->len = 0;
  m->mod_gen++;
}

/* zyxrt_set_is_subset reports whether every element of the first set is in the
   second. Both are maps whose value the caller never reads, so it tests
   membership through the same comparison the table uses. */
int zyxrt_set_is_subset(void *handle, void *other) {
  zyxrt_map *a = (zyxrt_map *)handle;
  zyxrt_map *b = (zyxrt_map *)other;
  if (a == NULL) return 1;
  if (b == NULL) return a->len == 0;
  for (size_t i = 0; i < a->cap; i++) {
    for (zyxrt_map_entry *e = a->buckets[i]; e != NULL; e = e->next) {
      if (find(b, e->key) == NULL) {
        return 0;
      }
    }
  }
  return 1;
}

/* keep only the entries a test accepts. The walk follows the link that points
   at each entry, so an entry it drops leaves the chain in one step. */
static void keep_where(zyxrt_map *a, zyxrt_map *b, int keep_in_both) {
  if (a == NULL) return;
  for (size_t i = 0; i < a->cap; i++) {
    zyxrt_map_entry **link = &a->buckets[i];
    while (*link != NULL) {
      zyxrt_map_entry *e = *link;
      int in_both = b != NULL && find(b, e->key) != NULL;
      if (in_both == keep_in_both) {
        link = &e->next;
        continue;
      }
      *link = e->next;
      a->len--;
      a->mod_gen++;
    }
  }
}

/* zyxrt_set_intersect keeps the elements both sets hold, and
   zyxrt_set_difference keeps the ones only the receiver holds. */
void zyxrt_set_intersect(void *handle, void *other) {
  keep_where((zyxrt_map *)handle, (zyxrt_map *)other, 1);
}

void zyxrt_set_difference(void *handle, void *other) {
  keep_where((zyxrt_map *)handle, (zyxrt_map *)other, 0);
}

/* zyxrt_set_union adds every element of the other set. */
void zyxrt_set_union(void *handle, void *other) {
  zyxrt_map *a = (zyxrt_map *)handle;
  zyxrt_map *b = (zyxrt_map *)other;
  if (a == NULL || b == NULL) return;
  unsigned char dummy = 0;
  for (size_t i = 0; i < b->cap; i++) {
    for (zyxrt_map_entry *e = b->buckets[i]; e != NULL; e = e->next) {
      zyxrt_map_set(a, e->key, &dummy);
    }
  }
}

/* An iteration walks the buckets in order and remembers where it was. The
   cursor lives in the region, so it is released with everything else, and the
   modification count makes a structural change during the walk a fault rather
   than a walk over storage the map no longer holds. */
typedef struct zyxrt_map_iter {
  zyxrt_map *map;
  size_t bucket;
  zyxrt_map_entry *entry;
} zyxrt_map_iter;

void *zyxrt_map_iter_new(void *handle) {
  zyxrt_map_iter *it = (zyxrt_map_iter *)zyxrt_region_alloc_current(sizeof(zyxrt_map_iter), 8);
  it->map = (zyxrt_map *)handle;
  it->bucket = 0;
  it->entry = NULL;
  return it;
}

/* zyxrt_map_iter_next advances to the next entry and copies its key and value
   into the caller's storage, answering whether there was one. */
int zyxrt_map_iter_next(void *handle, void *key, void *value) {
  zyxrt_map_iter *it = (zyxrt_map_iter *)handle;
  zyxrt_map *m = it == NULL ? NULL : it->map;
  if (m == NULL) return 0;
  if (it->entry != NULL) {
    it->entry = it->entry->next;
  }
  while (it->entry == NULL) {
    if (it->bucket >= m->cap) return 0;
    it->entry = m->buckets[it->bucket];
    it->bucket++;
  }
  if (key != NULL) memcpy(key, it->entry->key, m->key_size);
  if (value != NULL) memcpy(value, it->entry->value, m->val_size);
  return 1;
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
    zyxrt_array_set_len(list, m->len);
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
      m->mod_gen++;
      return 1;
    }
    link = &e->next;
  }
  return 0;
}

/* A set is a map whose value the caller never reads, so its iteration is the
   map's with the value left out. */
void *zyxrt_set_iter_new(void *handle) { return zyxrt_map_iter_new(handle); }

int zyxrt_set_iter_next(void *handle, void *elem) {
  return zyxrt_map_iter_next(handle, elem, NULL);
}
