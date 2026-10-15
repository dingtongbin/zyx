/* Public C ABI surface of the zyx runtime. */
#ifndef ZYXRUNTIME_H
#define ZYXRUNTIME_H

#include <stddef.h>
#include <stdint.h>

/* Regions. Every reference object belongs to exactly one region, and a
   region releases all of its objects at once. */
void *zyxrt_region_new(void);
void zyxrt_region_free(void *region);
void *zyxrt_region_alloc(void *region, size_t size, size_t align);
/* zyxrt_region_current returns the region of the running coroutine, which
   every object the coroutine creates belongs to. */
void *zyxrt_region_current(void);
/* zyxrt_region_alloc_current creates an object in the region of the running
   coroutine. */
void *zyxrt_region_alloc_current(size_t size, size_t align);
/* Containers. A container is an opaque handle whose buffer belongs to the
   region that created it. An element is stored as the bytes of its type, so
   every operation takes the address of the value it moves. */
void *zyxrt_array_new(size_t elem, size_t cap);
size_t zyxrt_array_len(void *handle);
void zyxrt_array_push(void *handle, const void *value);
int zyxrt_array_pop(void *handle, void *out);
int zyxrt_array_get(void *handle, size_t index, void *out);
void zyxrt_array_get_unchecked(void *handle, size_t index, void *out);
void zyxrt_array_remove(void *handle, size_t index);

/* A map hashes and compares a key through the two functions it is given, so
   the same table serves a scalar key, a string key, and a key of a type with
   its own Hash and Eq. */
void *zyxrt_map_new(size_t key_size, size_t val_size, void *hash, void *eq);
size_t zyxrt_map_len(void *handle);
void zyxrt_map_set(void *handle, const void *key, const void *value);
int zyxrt_map_get(void *handle, const void *key, void *out);
int zyxrt_map_contains(void *handle, const void *key);
int zyxrt_map_remove(void *handle, const void *key, void *out);
void zyxrt_map_keys(void *handle, void *out);
void zyxrt_map_values(void *handle, void *out);
void *zyxrt_map_key_list(void *handle, size_t key_size);

/* A priority queue is a heap ordered by a comparison the caller supplies. */
void *zyxrt_queue_new(size_t elem, void *less);
size_t zyxrt_queue_len(void *handle);
void zyxrt_queue_push(void *handle, const void *value);
int zyxrt_queue_pop(void *handle, void *out);

/* A blocking queue is bounded and ordered by a comparison the caller
   supplies. A push waits while it is full and a pop waits while it is
   empty, so coroutines hand work to one another through it. */
void *zyxrt_bqueue_new(size_t elem, size_t cap, void *less);
size_t zyxrt_bqueue_len(void *handle);
void zyxrt_bqueue_push(void *handle, const void *value);
int zyxrt_bqueue_try_push(void *handle, const void *value);
void zyxrt_bqueue_pop(void *handle, void *out);
int zyxrt_bqueue_try_pop(void *handle, void *out);

/* A set holds each element once. */
void *zyxrt_set_new(size_t elem_size, void *hash, void *eq);
size_t zyxrt_set_len(void *handle);
void zyxrt_set_add(void *handle, const void *elem);
int zyxrt_set_contains(void *handle, const void *elem);
int zyxrt_set_remove(void *handle, const void *elem, void *out);
/* The hashes and comparisons the language supplies for its own key types. */
uint64_t zyxrt_hash_bytes(const void *key, size_t size);
int zyxrt_eq_bytes(const void *a, const void *b, size_t size);
uint64_t zyxrt_hash_str_key(const void *key, size_t size);
int zyxrt_eq_str_key(const void *a, const void *b, size_t size);

/* An explicit region block makes its own region the one new objects belong
   to, and waits for the coroutines started in it before it releases them. */
void zyxrt_region_enter(void *region);
void zyxrt_region_leave(void);
void zyxrt_region_wait(void *region);

/* zyxrt_region_release returns one object to its region before the region
   exits, which is what a static release point does. The size names the size
   class, so no object carries a header. */
void zyxrt_region_release(void *region, void *object, size_t size);

/* Strings. A str is a borrowed pointer and its length, so equality compares
   the bytes rather than the two pointers. */
int8_t zyxrt_str_eq(const char *a, size_t an, const char *b, size_t bn);

/* Process entry. The generated main calls this with the address of the zyx
   main function, and the runtime runs it on the root region. */
typedef void (*zyxrt_main_fn)(void *arg);
int zyxrt_run_main(zyxrt_main_fn fn, void *arg);

/* Panic reporting and process abort. */
void zyxrt_panic(const char *msg, size_t len);

/* Output helpers, used by the standard library. */
void zyxrt_print_str(const char *p, size_t n);
void zyxrt_print_i64(int64_t v);
void zyxrt_print_u64(uint64_t v);
void zyxrt_print_f64(double v);
void zyxrt_print_bool(int8_t v);
void zyxrt_print_char(uint32_t c);
void zyxrt_print_nl(void);

/* Coroutines. A coroutine runs on its own stack and its own region. */
typedef void (*zyxrt_fn)(void *arg);
void *zyxrt_coro_spawn(zyxrt_fn fn, void *arg);
/* zyxrt_coro_join waits for a coroutine, reports how it ended, and releases
   it. It returns the value the coroutine recorded and writes the exception it
   raised to *panic, or NULL when it completed. The exception is read before
   the coroutine is released, so a caller never reaches freed memory. */
void *zyxrt_coro_join(void *handle, void **panic);
int zyxrt_coro_try_join(void *handle, void **result, void **panic);
/* zyxrt_sched_yield lets another ready coroutine run. A select uses it when
   no arm is ready and the select has no default. */
void zyxrt_sched_yield(void);
/* zyxrt_now returns the monotonic clock in nanoseconds, and zyxrt_sched_sleep
   suspends the running coroutine until that many nanoseconds have passed. A
   select uses the pair to honour its timeout without spinning. */
uint64_t zyxrt_now(void);
void zyxrt_sched_sleep(uint64_t ns);
/* zyxrt_coro_set_result records the value the running coroutine produced,
   which the handle's Join returns. A value that fits in a register crosses
   the boundary as itself; an object needs a copy, which the boundary marks
   settle. */
void zyxrt_coro_set_result(void *result);
/* zyxrt_coro_panic returns the exception a coroutine raised, or NULL. */
void *zyxrt_coro_panic(void *handle);
/* zyxrt_panic_message returns the text of an exception. */
const char *zyxrt_panic_message(const void *panic, size_t *len);

/* Deferred calls. A region records a mark on entry and runs the calls
   above it when it leaves, in reverse order. */
void *zyxrt_defer_mark(void);
void zyxrt_defer_push(void (*call)(void *), void *args);
void zyxrt_defer_run(void *mark);

/* Channels. A channel carries values of one size, up to a fixed capacity. */
void *zyxrt_chan_new(size_t elem, size_t cap);
void zyxrt_chan_send(void *ch, const void *value);
int zyxrt_chan_recv(void *ch, void *out);
int zyxrt_chan_try_send(void *ch, const void *value);
int zyxrt_chan_try_recv(void *ch, void *out);
void zyxrt_chan_close(void *ch);

#endif /* ZYXRUNTIME_H */
