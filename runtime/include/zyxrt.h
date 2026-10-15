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
void *zyxrt_coro_join(void *handle);
int zyxrt_coro_try_join(void *handle, void **result);
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
