/* The socket calls of std/net. Every descriptor this package makes is
   non-blocking, so a call that has nothing to do answers a would-block failure
   and the language parks the coroutine on the runtime's event loop rather than
   occupying a scheduler thread. The address a program names crosses as the
   text of the host and the number of the port, and the descriptor crosses as
   the one integer the runtime's loop watches. */

#ifndef ZYXNET_H
#define ZYXNET_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. A call reports a failure by leaving one
   of these, the platform's own error number, and the text of the platform's
   message behind, which the caller reads with the three queries below. A
   thread keeps its own, so a failure belongs to the call that made it. */
enum {
  ZYXNET_NOT_FOUND = 1,
  ZYXNET_PERMISSION = 2,
  ZYXNET_EXISTS = 3,
  ZYXNET_NOT_DIR = 4,
  ZYXNET_IS_DIR = 5,
  ZYXNET_INVALID = 6,
  ZYXNET_CLOSED = 7,
  ZYXNET_TIMEOUT = 8,
  ZYXNET_INTERRUPTED = 9,
  ZYXNET_WOULD_BLOCK = 10,
  ZYXNET_EOF = 11,
  ZYXNET_ADDR_IN_USE = 12,
  ZYXNET_ADDR_NOT_AVAILABLE = 13,
  ZYXNET_REFUSED = 14,
  ZYXNET_RESET = 15,
  ZYXNET_UNREACHABLE = 16,
  ZYXNET_OTHER = 17
};

int32_t zyxnet_error_kind(void);
int32_t zyxnet_error_number(void);
/* zyxnet_error_message appends the text of the last failure to a buffer. */
void zyxnet_error_message(void *buf);

/* zyxnet_start makes the platform's socket library ready. It is idempotent
   and every call that makes a socket runs it first. */
int32_t zyxnet_start(void);

/* A listener, a stream connection, and a datagram socket are all one handle:
   the descriptor, the family it belongs to, and which of the three it is. A
   call that answers nothing reports a failure through the queries above. */
void *zyxnet_listen(const char *network, size_t network_len, const char *host,
                    size_t host_len, int32_t port);
void *zyxnet_accept(void *listener);
void *zyxnet_dial(const char *network, size_t network_len, const char *host,
                  size_t host_len, int32_t port);
/* zyxnet_connect_pending says whether the connection a dial made is still
   travelling. The caller waits for the descriptor to become writable and then
   reads zyxnet_connect_state, which answers 0 when the connection is up and -1
   with a failure when it did not come up. */
int32_t zyxnet_connect_pending(void *conn);
int32_t zyxnet_connect_state(void *conn);
void *zyxnet_bind_udp(const char *host, size_t host_len, int32_t port);

/* zyxnet_fd answers the descriptor the runtime's event loop watches. */
int32_t zyxnet_fd(void *handle);
void zyxnet_close(void *handle);
int32_t zyxnet_shutdown(void *handle, int32_t how);

/* zyxnet_local_addr and zyxnet_remote_addr append the address of the near and
   the far end to a buffer as host:port, or as the path of a unix socket. */
int32_t zyxnet_local_addr(void *handle, void *buf);
int32_t zyxnet_remote_addr(void *handle, void *buf);

/* zyxnet_read moves at most cap bytes into the language's own byte buffer and
   answers how many; zero is the end of the stream. zyxnet_send_to and
   zyxnet_recv_from are the datagram forms, and the source of a datagram is
   appended to the address buffer. */
int64_t zyxnet_recv(void *handle, void *buf, size_t cap);
int64_t zyxnet_send(void *handle, const unsigned char *buf, size_t len);
int64_t zyxnet_recv_from(void *handle, void *buf, size_t cap, void *addr);
int64_t zyxnet_send_to(void *handle, const unsigned char *buf, size_t len,
                       const char *host, size_t host_len, int32_t port);

int32_t zyxnet_set_blocking(void *handle, int32_t on);
int32_t zyxnet_set_no_delay(void *handle, int32_t on);
int32_t zyxnet_set_keep_alive(void *handle, int32_t on);

/* zyxnet_resolve appends the numeric addresses a name answers with to a
   buffer, each followed by a zero byte. */
int32_t zyxnet_resolve(const char *host, size_t host_len, void *buf);

#endif /* ZYXNET_H */
