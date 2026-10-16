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

/* Buf is a mutable byte buffer. Bytes reports the live bytes in place, which
   is the view a caller reads; Consume drops bytes from the front in constant
   time, and Take hands them to an array. */
void *zyxrt_buf_new(size_t cap);
size_t zyxrt_buf_len(void *handle);
size_t zyxrt_buf_cap(void *handle);
void zyxrt_buf_clear(void *handle);
void zyxrt_buf_reserve(void *handle, size_t n);
void zyxrt_buf_write(void *handle, const unsigned char *p, size_t n);
void zyxrt_buf_write_byte(void *handle, unsigned char value);
void zyxrt_buf_consume(void *handle, size_t n);
void zyxrt_buf_bytes(void *handle, const unsigned char **out, size_t *len);
void zyxrt_buf_str(void *handle, const char **out, size_t *len);
void *zyxrt_buf_take(void *handle);

/* Ring is a fixed-capacity circular buffer. A push into a full ring drops the
   oldest element, and a pop takes the oldest, so both are constant time. */
void *zyxrt_ring_new(size_t elem, size_t cap);
size_t zyxrt_ring_len(void *handle);
size_t zyxrt_ring_cap(void *handle);
void zyxrt_ring_push(void *handle, const void *value);
int zyxrt_ring_pop(void *handle, void *out);

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

/* The text of one value, in the single form the language writes it. Each
   function renders into a buffer the caller owns and reports the byte count.
   Every sink for a value's text goes through these, so printing and writing
   into a buffer can never disagree. */
size_t zyxrt_fmt_i64(char *out, size_t cap, int64_t v);
size_t zyxrt_fmt_u64(char *out, size_t cap, uint64_t v);
size_t zyxrt_fmt_f64(char *out, size_t cap, double v);
size_t zyxrt_fmt_bool(char *out, size_t cap, int8_t v);
size_t zyxrt_fmt_char(char *out, size_t cap, uint32_t c);
/* A 128-bit value crosses as its two halves, because the runtime is portable
   C11 and has no 128-bit type of its own. */
size_t zyxrt_fmt_i128(char *out, size_t cap, int64_t hi, uint64_t lo);
size_t zyxrt_fmt_u128(char *out, size_t cap, uint64_t hi, uint64_t lo);

/* The text of one value is appended to a buffer, so a value costs one pass
   and never becomes a string of its own. */
void zyxrt_buf_write_i64(void *handle, int64_t v);
void zyxrt_buf_write_u64(void *handle, uint64_t v);
void zyxrt_buf_write_f64(void *handle, double v);
void zyxrt_buf_write_bool(void *handle, int8_t v);
void zyxrt_buf_write_char(void *handle, uint32_t c);
void zyxrt_buf_write_dur(void *handle, uint64_t ns);
void zyxrt_buf_write_i128(void *handle, int64_t hi, uint64_t lo);
void zyxrt_buf_write_u128(void *handle, uint64_t hi, uint64_t lo);

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
/* zyxrt_watch_fn is a supervisor: it takes the receiver of the function value
   the program wrote and the exception the coroutine raised. */
typedef void (*zyxrt_watch_fn)(void *recv, void *panic);
void *zyxrt_coro_spawn(zyxrt_fn fn, void *arg);
/* zyxrt_coro_spawn_watched starts a coroutine and binds a supervisor to it.
   The supervisor runs when the coroutine raises, before the coroutine's
   region is released, so the exception it is given is still valid. */
void *zyxrt_coro_spawn_watched(zyxrt_fn fn, void *arg, zyxrt_watch_fn watch, void *watch_recv);
/* zyxrt_coro_join waits for a coroutine and reports how it ended. It returns
   the value the coroutine recorded and writes the exception it raised to
   *panic, or NULL when it completed. The coroutine is released when the
   region that adopted it is released, not here: a handle is a value, so a
   copy of it is a second name for the same coroutine, and joining twice
   answers the same way rather than reaching freed memory. */
void *zyxrt_coro_join(void *handle, void **panic);
/* zyxrt_coro_try_join answers the same way, and reports whether the coroutine
   has finished. */
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

/* The sync primitives. Each one parks the coroutine that must wait, so a
   wait costs no scheduler thread. A wait group counts the coroutines a
   waiter waits for; a once runs one function exactly once, with the callers
   that did not run it waiting for it to finish; a semaphore holds a count of
   permits. */
void *zyxrt_wg_new(void);
void zyxrt_wg_add(void *wg, int64_t n);
void zyxrt_wg_wait(void *wg);
void *zyxrt_once_new(void);
/* zyxrt_once_enter answers whether the caller runs the function, and
   zyxrt_once_leave records that it has run. */
int zyxrt_once_enter(void *once);
void zyxrt_once_leave(void *once);
void *zyxrt_sem_new(int64_t n);
void zyxrt_sem_acquire(void *sem);
int zyxrt_sem_try_acquire(void *sem);
void zyxrt_sem_release(void *sem);
/* A mutex is held by one coroutine at a time. zyxrt_mutex_wait releases the
   lock and parks the coroutine until a notification arrives, then takes the
   lock again, which is what makes the monitor methods a condition variable. */
void *zyxrt_mutex_new(void);
void zyxrt_mutex_lock(void *mutex);
int zyxrt_mutex_try_lock(void *mutex);
void zyxrt_mutex_unlock(void *mutex);
void zyxrt_mutex_wait(void *mutex);
void zyxrt_mutex_notify_one(void *mutex);
void zyxrt_mutex_notify_all(void *mutex);
/* Channels. A channel carries values of one size, up to a fixed capacity. */
void *zyxrt_chan_new(size_t elem, size_t cap);
void zyxrt_chan_send(void *ch, const void *value);
int zyxrt_chan_recv(void *ch, void *out);
int zyxrt_chan_try_send(void *ch, const void *value);
int zyxrt_chan_try_recv(void *ch, void *out);
/* zyxrt_chan_recv_ready and zyxrt_chan_send_ready report whether an operation
   would not block, which is what a select arm asks. A receive on a closed
   channel is ready: it answers with the zero value and false. A send on one
   is not, because a send that cannot proceed is the panic the language
   defines, and a select arm that cannot proceed is skipped. */
int zyxrt_chan_recv_ready(void *ch);
int zyxrt_chan_send_ready(void *ch);
/* zyxrt_select_wait parks the running coroutine until one of the watched
   channels is ready or the deadline passes, so a select waits instead of
   spinning. recv_arms marks which channels are watched for a receive; a
   deadline of zero means the select has no timeout. */
void zyxrt_select_wait(void *const *handles, const int *recv_arms, size_t n, uint64_t deadline);
void zyxrt_chan_close(void *ch);

#endif /* ZYXRUNTIME_H */
