/* The hiredis binding of the redis client. The reply tree a command answers is
   the library's, so the package reads it through this surface rather than through
   the library's own types: one call reaches a command and a handful of accessors
   walk what came back. */

#ifndef ZYXREDIS_H
#define ZYXREDIS_H

#include <stddef.h>
#include <stdint.h>

/* The kinds of failure, numbered so that the package's own kind list can be read
   from them: an address that is not host:port, a connection the server refused,
   a connection that is done with, and anything else. */
enum {
  ZYXREDIS_INVALID = 1,
  ZYXREDIS_REFUSED = 2,
  ZYXREDIS_CLOSED = 3,
  ZYXREDIS_OTHER = 4
};

/* zyxredis_connect answers a connection to the host and port the address names,
   as host:port, or a handle nothing is read through when it could not be made.
   Whether it was made is what zyxredis_error_kind answers: zero means it was. */
void *zyxredis_connect(const char *addr, size_t addr_len);

/* zyxredis_error_kind, zyxredis_error_number and zyxredis_error_message describe
   the last call: a kind of zero means it succeeded, the number is the system's
   when there is one, and the message appends its text to a buffer. */
int32_t zyxredis_error_kind(void);
int32_t zyxredis_error_number(void);
void zyxredis_error_message(void *buf);

/* zyxredis_close gives the connection up. */
void zyxredis_close(void *handle);

/* zyxredis_do runs one command with its arguments and answers the reply, or a
   reply of no shape when it failed, which the error entries above explain. The
   arguments are the bytes of a buffer whose entries are separated by a zero byte,
   the way the other bindings pass a list of texts, so an argument may not itself
   hold one. */
void *zyxredis_do(void *handle, const char *cmd, size_t cmd_len, const char *args,
                  size_t args_len);

/* zyxredis_reply_free gives a reply up, and with it every reply it holds. */
void zyxredis_reply_free(void *reply);

/* zyxredis_reply_kind answers the shape of a reply: 1 status, 2 error, 3 an
   integer, 4 a bulk string, 5 nil, 6 an array, and 0 for a reply this package
   does not know. */
int32_t zyxredis_reply_kind(void *reply);

/* zyxredis_reply_integer answers the number of an integer reply. */
int64_t zyxredis_reply_integer(void *reply);

/* zyxredis_reply_text_into appends the bytes of a status, error or bulk reply to
   a buffer, which is how a text travels out of a reply that is about to be given
   up. */
void zyxredis_reply_text_into(void *reply, void *buf);

/* zyxredis_reply_count answers how many elements an array reply holds. */
size_t zyxredis_reply_count(void *reply);

/* zyxredis_reply_at answers one element of an array reply, which lives as long as
   the reply it came from. */
void *zyxredis_reply_at(void *reply, size_t index);

#endif
