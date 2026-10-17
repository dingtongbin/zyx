/* The hiredis binding of the redis client: the library's own reply tree is the
   one the accessors above walk, the address is split here so that a program
   states one text rather than a host and a port, and every failure is left where
   the error entries read it, the way the socket binding leaves its own. */

#include "zyxredis.h"

#include <stdlib.h>
#include <string.h>

#include <hiredis/hiredis.h>

#include "zyxrt.h"

/* The port a redis server listens on when the address names none. */
#define ZYXREDIS_DEFAULT_PORT 6379

/* The room a host name is given, which is what a text address may carry: a longer
   one is refused rather than cut. */
#define ZYXREDIS_MAX_HOST 256

/* The room a failure's text is given. */
#define ZYXREDIS_MAX_MESSAGE 256

/* What went wrong the last time. The socket binding keeps its failure the same
   way, and a program reads it before making another call. */
static int32_t last_kind;
static int32_t last_number;
static char last_message[ZYXREDIS_MAX_MESSAGE];

static void report(int32_t kind, int32_t number, const char *message) {
  last_kind = kind;
  last_number = number;
  if (message == NULL) {
    message = "";
  }
  strncpy(last_message, message, sizeof(last_message) - 1);
  last_message[sizeof(last_message) - 1] = 0;
}

int32_t zyxredis_error_kind(void) { return last_kind; }
int32_t zyxredis_error_number(void) { return last_number; }

void zyxredis_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)last_message, strlen(last_message));
}

void *zyxredis_connect(const char *addr, size_t addr_len) {
  char host[ZYXREDIS_MAX_HOST];
  size_t host_len = 0;
  long port = ZYXREDIS_DEFAULT_PORT;
  size_t at = 0;
  report(0, 0, "");
  while (at < addr_len && addr[at] != ':') {
    if (host_len + 1 >= sizeof(host)) {
      report(ZYXREDIS_INVALID, 0, "the host name is longer than an address may be");
      return NULL;
    }
    host[host_len++] = addr[at++];
  }
  host[host_len] = 0;
  if (at < addr_len && addr[at] == ':') {
    long value = 0;
    int digits = 0;
    at++;
    while (at < addr_len && addr[at] >= '0' && addr[at] <= '9') {
      value = value * 10 + (addr[at] - '0');
      at++;
      digits = 1;
    }
    if (!digits || at != addr_len || value <= 0 || value > 65535) {
      report(ZYXREDIS_INVALID, 0, "the port is not a number an address may name");
      return NULL;
    }
    port = value;
  }
  if (host_len == 0) {
    report(ZYXREDIS_INVALID, 0, "the address names no host");
    return NULL;
  }
  redisContext *c = redisConnect(host, (int)port);
  if (c == NULL) {
    report(ZYXREDIS_OTHER, 0, "the connection could not be made");
    return NULL;
  }
  if (c->err != 0) {
    report(ZYXREDIS_REFUSED, c->err, c->errstr);
  }
  return (void *)c;
}

void zyxredis_close(void *handle) {
  if (handle == NULL) {
    return;
  }
  redisFree((redisContext *)handle);
}

void *zyxredis_do(void *handle, const char *cmd, size_t cmd_len, const char *args,
                  size_t args_len) {
  redisContext *c = (redisContext *)handle;
  report(0, 0, "");
  if (c == NULL) {
    report(ZYXREDIS_CLOSED, 0, "the connection was not made");
    return NULL;
  }
  /* The command is the first argument and every zero-separated entry after it is
     one more, so a buffer of nothing is a command with no arguments. */
  size_t count = 1;
  size_t i;
  for (i = 0; i < args_len; i++) {
    if (args[i] == 0) {
      count++;
    }
  }
  const char **argv = (const char **)malloc(count * sizeof(char *));
  size_t *lens = (size_t *)malloc(count * sizeof(size_t));
  if (argv == NULL || lens == NULL) {
    free(argv);
    free(lens);
    report(ZYXREDIS_OTHER, 0, "the argument list could not be built");
    return NULL;
  }
  argv[0] = cmd;
  lens[0] = cmd_len;
  size_t at = 0;
  size_t filled = 1;
  for (i = 0; i < args_len; i++) {
    if (args[i] != 0) {
      continue;
    }
    argv[filled] = args + at;
    lens[filled] = i - at;
    filled++;
    at = i + 1;
  }
  redisReply *reply = (redisReply *)redisCommandArgv(c, (int)filled, argv, lens);
  free(argv);
  free(lens);
  if (reply == NULL) {
    report(ZYXREDIS_REFUSED, c->err, c->errstr);
  }
  return (void *)reply;
}

void zyxredis_reply_free(void *reply) {
  if (reply == NULL) {
    return;
  }
  freeReplyObject((redisReply *)reply);
}

int32_t zyxredis_reply_kind(void *reply) {
  redisReply *r = (redisReply *)reply;
  if (r == NULL) {
    return 0;
  }
  switch (r->type) {
    case REDIS_REPLY_STATUS:
      return 1;
    case REDIS_REPLY_ERROR:
      return 2;
    case REDIS_REPLY_INTEGER:
      return 3;
    case REDIS_REPLY_STRING:
      return 4;
    case REDIS_REPLY_NIL:
      return 5;
    case REDIS_REPLY_ARRAY:
      return 6;
  }
  return 0;
}

int64_t zyxredis_reply_integer(void *reply) {
  redisReply *r = (redisReply *)reply;
  if (r == NULL) {
    return 0;
  }
  return (int64_t)r->integer;
}

void zyxredis_reply_text_into(void *reply, void *buf) {
  redisReply *r = (redisReply *)reply;
  if (r == NULL || r->str == NULL) {
    return;
  }
  zyxrt_buf_write(buf, (const unsigned char *)r->str, r->len);
}

size_t zyxredis_reply_count(void *reply) {
  redisReply *r = (redisReply *)reply;
  if (r == NULL) {
    return 0;
  }
  return r->elements;
}

void *zyxredis_reply_at(void *reply, size_t index) {
  redisReply *r = (redisReply *)reply;
  if (r == NULL || index >= r->elements) {
    return NULL;
  }
  return (void *)r->element[index];
}
