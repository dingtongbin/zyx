/* The TLS calls of std/tls: the context a configuration builds, the session
   that secures a stream, and the failure the last call reported.

   OpenSSL holds a session, and the bytes of the stream travel through the two
   buffers of that session rather than through a socket OpenSSL owns. So a call
   that cannot go on answers which direction it waits for, and the caller
   moves the bytes through the stream it was given, which is what lets a
   coroutine park on the stream the way it parks on any other read or write. */

#ifndef ZYXTLS_H
#define ZYXTLS_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. */
enum {
  ZYXTLS_NOT_FOUND = 1,
  ZYXTLS_PERMISSION = 2,
  ZYXTLS_EXISTS = 3,
  ZYXTLS_NOT_DIR = 4,
  ZYXTLS_IS_DIR = 5,
  ZYXTLS_INVALID = 6,
  ZYXTLS_CLOSED = 7,
  ZYXTLS_TIMEOUT = 8,
  ZYXTLS_INTERRUPTED = 9,
  ZYXTLS_WOULD_BLOCK = 10,
  ZYXTLS_EOF = 11,
  ZYXTLS_ADDR_IN_USE = 12,
  ZYXTLS_ADDR_NOT_AVAILABLE = 13,
  ZYXTLS_REFUSED = 14,
  ZYXTLS_RESET = 15,
  ZYXTLS_UNREACHABLE = 16,
  ZYXTLS_OTHER = 17
};

/* The answer of a call that could not finish: it waits for bytes from the
   peer, or for room to send them, or it failed, or the peer ended the
   session. */
enum {
  ZYXTLS_WANT_READ = -1,
  ZYXTLS_WANT_WRITE = -2,
  ZYXTLS_FAILED = -3,
  ZYXTLS_ENDED = -4
};

int32_t zyxtls_error_kind(void);
/* zyxtls_error_number answers the number of the failure the last call
   reported: the number the system gave when the failure is one the system
   names, the code of the certificate check that refused the peer, or zero. */
int32_t zyxtls_error_number(void);
void zyxtls_error_message(void *buf);

/* zyxtls_ctx_new builds a context: one that secures a server when server is
   one, and one that secures a client otherwise. */
void *zyxtls_ctx_new(int32_t server);
/* zyxtls_ctx_cert and zyxtls_ctx_key name the certificate chain a server
   answers with and the key that belongs to it. zyxtls_ctx_ca names the
   certificates a client trusts, and zyxtls_ctx_min_version sets the lowest
   protocol version the context will speak, where one is TLS 1.2 and two is
   TLS 1.3. */
int32_t zyxtls_ctx_cert(void *ctx, const char *path, size_t len);
int32_t zyxtls_ctx_key(void *ctx, const char *path, size_t len);
int32_t zyxtls_ctx_ca(void *ctx, const char *path, size_t len);
int32_t zyxtls_ctx_min_version(void *ctx, int32_t version);
void zyxtls_ctx_free(void *ctx);

/* zyxtls_conn_new builds one session over a context. A client names the host
   it asks for with zyxtls_conn_sni, and zyxtls_conn_verify asks that the
   certificate of the peer be checked against the trusted certificates with
   that name. A client that asked for zyxtls_conn_trust_none checks
   nothing. */
void *zyxtls_conn_new(void *ctx, int32_t server);
void zyxtls_conn_free(void *conn);
int32_t zyxtls_conn_verify(void *conn, const char *name, size_t len);
int32_t zyxtls_conn_trust_none(void *conn);
int32_t zyxtls_conn_sni(void *conn, const char *name, size_t len);

/* zyxtls_handshake runs the greeting until the session is secured, and
   answers one, or one of the codes above. */
int32_t zyxtls_handshake(void *conn);
/* zyxtls_read reads at most max bytes of the peer into the language's own
   buffer and answers how many, or one of the codes above; a session the peer
   closed answers ZYXTLS_CLOSED. */
int32_t zyxtls_read(void *conn, void *buf, size_t max);
int32_t zyxtls_write(void *conn, const unsigned char *bytes, size_t len);
/* zyxtls_flush appends what the session wants to send to a buffer, and
   zyxtls_feed gives the session the bytes that arrived. */
void zyxtls_flush(void *conn, void *buf);
int32_t zyxtls_feed(void *conn, const unsigned char *bytes, size_t len);
/* zyxtls_shutdown tells the peer that the session is over, which sends what
   the session still wants to send. */
int32_t zyxtls_shutdown(void *conn);

#endif /* ZYXTLS_H */
