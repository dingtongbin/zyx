/* Public C ABI surface of the zyx runtime. */
#ifndef ZYXRUNTIME_H
#define ZYXRUNTIME_H

#include <stddef.h>
#include <stdint.h>

/* Regions. Every reference object belongs to exactly one region, and a
   region releases all of its objects at once. */
void *zyxrt_region_new(void);
void zyxrt_region_free(void *region);
void *zyxrt_alloc(void *region, size_t size, size_t align);
/* zyxrt_region_current returns the region of the running coroutine, which
   every object the coroutine creates belongs to. */
void *zyxrt_region_current(void);
/* zyxrt_alloc_here creates an object in the region of the running
   coroutine. */
void *zyxrt_alloc_here(size_t size, size_t align);

/* Process entry. The generated main calls this with the address of the zyx
   main function, and the runtime runs it on the root region. */
typedef void (*zyxrt_main_fn)(void *arg);
int zyxrt_run(zyxrt_main_fn fn, void *arg);

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
void *zyxrt_spawn(zyxrt_fn fn, void *arg);
void *zyxrt_join(void *handle);
int zyxrt_try_join(void *handle, void **result);

/* Channels. A channel carries values of one size, up to a fixed capacity. */
void *zyxrt_chan_new(size_t elem, size_t cap);
void zyxrt_chan_send(void *ch, const void *value);
int zyxrt_chan_recv(void *ch, void *out);
int zyxrt_chan_try_send(void *ch, const void *value);
int zyxrt_chan_try_recv(void *ch, void *out);
void zyxrt_chan_close(void *ch);

#endif /* ZYXRUNTIME_H */
