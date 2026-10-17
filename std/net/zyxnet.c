#include "zyxnet.h"

#include "zyxrt.h"

/* The C compiler is asked for strict ISO C, under which a system hides the
   declarations this file is written against. The word has to come before any
   system header, so it is written here first. */
#if !defined(_WIN32)
#if !defined(_GNU_SOURCE)
#define _GNU_SOURCE 1
#endif
#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif
#endif

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#if defined(_WIN32)

#if !defined(_WIN32_WINNT) || _WIN32_WINNT < 0x0600
#undef _WIN32_WINNT
#define _WIN32_WINNT 0x0600
#endif

#include <winsock2.h>
#include <ws2tcpip.h>

#if defined(__has_include)
#if __has_include(<afunix.h>)
#include <afunix.h>
#endif
#endif

typedef SOCKET zyxnet_descriptor;
typedef int zyxnet_addrlen;
#define ZYXNET_BAD ((zyxnet_descriptor)INVALID_SOCKET)
#define ZYXNET_CLOSE(fd) closesocket(fd)
/* The number an interrupted call reports, which is the socket library's own
   on Windows and the C runtime's elsewhere. */
#define ZYXNET_INTERRUPTED_NUMBER WSAEINTR
/* How a half close names the direction it refuses. The socket interface of
   Windows spells the three its own way. */
#define ZYXNET_SHUT_READ SD_RECEIVE
#define ZYXNET_SHUT_WRITE SD_SEND
#define ZYXNET_SHUT_BOTH SD_BOTH

#else

#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

typedef int zyxnet_descriptor;
typedef socklen_t zyxnet_addrlen;
#define ZYXNET_BAD (-1)
#define ZYXNET_CLOSE(fd) close(fd)
#define ZYXNET_INTERRUPTED_NUMBER EINTR
#define ZYXNET_SHUT_READ SHUT_RD
#define ZYXNET_SHUT_WRITE SHUT_WR
#define ZYXNET_SHUT_BOTH SHUT_RDWR

#endif

/* What a handle is: a descriptor, the family it belongs to, and which of the
   three shapes it has. The descriptor is kept as the platform's own type and
   crosses to the language as the one integer the runtime's loop watches. */
#define ZYXNET_STREAM 0
#define ZYXNET_LISTENER 1
#define ZYXNET_DATAGRAM 2

typedef struct zyxnet_socket {
  zyxnet_descriptor descriptor;
  int family;
  int kind;
  int pending;
} zyxnet_socket;

/* The failure state of the calling thread. A call reports a failure by
   leaving what went wrong here, which the caller reads at once. */
static _Thread_local int32_t zyxnet_kind;
static _Thread_local int32_t zyxnet_errno;
static _Thread_local char zyxnet_text[256];

/* zyxnet_last_error answers the error number the platform left behind. Windows
   keeps the socket library's own number apart from the C runtime's. */
static int zyxnet_last_error(void) {
#if defined(_WIN32)
  return (int)WSAGetLastError();
#else
  return errno;
#endif
}

static void zyxnet_fail(int32_t kind, int number) {
  zyxnet_kind = kind;
  zyxnet_errno = number;
#if defined(_WIN32)
  snprintf(zyxnet_text, sizeof(zyxnet_text), "socket error %d", number);
#else
  const char *text = strerror(number);
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  if (n >= sizeof(zyxnet_text)) n = sizeof(zyxnet_text) - 1;
  memcpy(zyxnet_text, text, n);
  zyxnet_text[n] = 0;
#endif
}

static void zyxnet_clear(void) {
  zyxnet_kind = 0;
  zyxnet_errno = 0;
  zyxnet_text[0] = 0;
}

/* zyxnet_kind_of maps the platform's own error number onto the shape every
   system package reports. The numbers are the platform's, so the mapping is
   written where the numbers are. */
#if defined(_WIN32)

static int32_t zyxnet_kind_of(int number) {
  switch (number) {
    case WSAEACCES:
      return ZYXNET_PERMISSION;
    case WSAEINVAL:
      return ZYXNET_INVALID;
    case WSAEBADF:
    case WSAENOTSOCK:
      return ZYXNET_CLOSED;
    case WSAETIMEDOUT:
      return ZYXNET_TIMEOUT;
    case WSAEINTR:
      return ZYXNET_INTERRUPTED;
    case WSAEWOULDBLOCK:
    case WSAEINPROGRESS:
    case WSAEALREADY:
      return ZYXNET_WOULD_BLOCK;
    case WSAEADDRINUSE:
      return ZYXNET_ADDR_IN_USE;
    case WSAEADDRNOTAVAIL:
    case WSAEAFNOSUPPORT:
      return ZYXNET_ADDR_NOT_AVAILABLE;
    case WSAECONNREFUSED:
      return ZYXNET_REFUSED;
    case WSAECONNRESET:
    case WSAECONNABORTED:
      return ZYXNET_RESET;
    case WSAENETUNREACH:
    case WSAEHOSTUNREACH:
    case WSAENETDOWN:
      return ZYXNET_UNREACHABLE;
    default:
      return ZYXNET_OTHER;
  }
}

static int zyxnet_in_progress(int number) {
  return number == WSAEWOULDBLOCK || number == WSAEINPROGRESS ||
         number == WSAEALREADY || number == WSAEINTR;
}

#else

static int32_t zyxnet_kind_of(int number) {
  switch (number) {
    case ENOENT:
      return ZYXNET_NOT_FOUND;
    case EACCES:
    case EPERM:
      return ZYXNET_PERMISSION;
    case EEXIST:
      return ZYXNET_EXISTS;
    case ENOTDIR:
      return ZYXNET_NOT_DIR;
    case EISDIR:
      return ZYXNET_IS_DIR;
    case EINVAL:
      return ZYXNET_INVALID;
    case EBADF:
      return ZYXNET_CLOSED;
    case ETIMEDOUT:
      return ZYXNET_TIMEOUT;
    case EINTR:
      return ZYXNET_INTERRUPTED;
#if defined(EWOULDBLOCK) && EWOULDBLOCK != EAGAIN
    case EWOULDBLOCK:
#endif
    case EAGAIN:
      return ZYXNET_WOULD_BLOCK;
    case EADDRINUSE:
      return ZYXNET_ADDR_IN_USE;
    case EADDRNOTAVAIL:
    case EAFNOSUPPORT:
      return ZYXNET_ADDR_NOT_AVAILABLE;
    case ECONNREFUSED:
      return ZYXNET_REFUSED;
    case ECONNRESET:
    case EPIPE:
      return ZYXNET_RESET;
    case ENETUNREACH:
    case EHOSTUNREACH:
    case ENETDOWN:
      return ZYXNET_UNREACHABLE;
    default:
      return ZYXNET_OTHER;
  }
}

static int zyxnet_in_progress(int number) {
  return number == EINPROGRESS || number == EAGAIN || number == EINTR;
}

#endif

static void zyxnet_fail_from(int number) {
  zyxnet_fail(zyxnet_kind_of(number), number);
}

int32_t zyxnet_error_kind(void) { return zyxnet_kind; }

int32_t zyxnet_error_number(void) { return zyxnet_errno; }

void zyxnet_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxnet_text, strlen(zyxnet_text));
}

/* zyxnet_ncopy copies the bytes of a text into a terminated string, because
   the platform's calls want one and a str of the language is a view without a
   terminator. */
static char *zyxnet_ncopy(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  if (len > 0 && text != NULL) memcpy(out, text, len);
  out[len] = 0;
  return out;
}

int32_t zyxnet_start(void) {
#if defined(_WIN32)
  static int started;
  if (started) return 0;
  WSADATA data;
  if (WSAStartup(MAKEWORD(2, 2), &data) != 0) {
    zyxnet_fail(ZYXNET_OTHER, zyxnet_last_error());
    return -1;
  }
  started = 1;
#endif
  return 0;
}

/* zyxnet_nonblocking sets the descriptor's blocking mode. A handle of this
   package is non-blocking, so every wait goes through the event loop. */
static int zyxnet_nonblocking(zyxnet_descriptor descriptor, int on) {
#if defined(_WIN32)
  u_long value = on ? 0u : 1u;
  return ioctlsocket(descriptor, FIONBIO, &value);
#else
  int flags = fcntl(descriptor, F_GETFL, 0);
  if (flags < 0) return -1;
  if (on) {
    flags &= ~O_NONBLOCK;
  } else {
    flags |= O_NONBLOCK;
  }
  return fcntl(descriptor, F_SETFL, flags);
#endif
}

/* zyxnet_wrap builds the handle of a descriptor. */
static void *zyxnet_wrap(zyxnet_descriptor descriptor, int family, int kind) {
  zyxnet_socket *socket = (zyxnet_socket *)malloc(sizeof(zyxnet_socket));
  if (socket == NULL) {
    ZYXNET_CLOSE(descriptor);
    zyxnet_fail(ZYXNET_OTHER, 0);
    return NULL;
  }
  socket->descriptor = descriptor;
  socket->family = family;
  socket->kind = kind;
  socket->pending = 0;
  return socket;
}

/* zyxnet_network reads the network text. It answers 0 and fills the socket
   type and the family the text asks for, or -1 for a text this package does
   not serve. A family of AF_UNSPEC lets the address decide between the two
   internet families. */
static int zyxnet_network(const char *network, size_t len, int *socktype,
                          int *family, int *unix_domain) {
  *socktype = SOCK_STREAM;
  *family = AF_UNSPEC;
  *unix_domain = 0;
  if (len == 3 && memcmp(network, "tcp", 3) == 0) return 0;
  if (len == 4 && memcmp(network, "tcp4", 4) == 0) {
    *family = AF_INET;
    return 0;
  }
  if (len == 4 && memcmp(network, "tcp6", 4) == 0) {
    *family = AF_INET6;
    return 0;
  }
#if defined(AF_UNIX)
  if (len == 4 && memcmp(network, "unix", 4) == 0) {
    *family = AF_UNIX;
    *unix_domain = 1;
    return 0;
  }
#endif
  return -1;
}

/* zyxnet_unix_addr writes the address of a unix socket path. */
#if defined(AF_UNIX)
static int zyxnet_unix_addr(const char *path, struct sockaddr_storage *out,
                            zyxnet_addrlen *out_len) {
  struct sockaddr_un *un = (struct sockaddr_un *)out;
  size_t len = strlen(path);
  if (len == 0 || len >= sizeof(un->sun_path)) {
    zyxnet_fail(ZYXNET_INVALID, 0);
    return -1;
  }
  memset(un, 0, sizeof(*un));
  un->sun_family = AF_UNIX;
  memcpy(un->sun_path, path, len);
  *out_len = (zyxnet_addrlen)(offsetof(struct sockaddr_un, sun_path) + len + 1);
  return 0;
}
#endif

/* zyxnet_internet_addr writes the address a host and a port name. A host that
   is a numeric address is read as one, and a name is looked up by the system,
   which is the one call here that reaches a name service. */
static int zyxnet_internet_addr(const char *host, int family, int socktype,
                                int32_t port, int passive,
                                struct sockaddr_storage *out,
                                zyxnet_addrlen *out_len) {
  char service[16];
  snprintf(service, sizeof(service), "%d", (int)port);
  struct addrinfo hints;
  memset(&hints, 0, sizeof(hints));
  hints.ai_family = family;
  hints.ai_socktype = socktype;
  hints.ai_flags = AI_NUMERICSERV;
  if (passive || host == NULL || host[0] == 0) hints.ai_flags |= AI_PASSIVE;
  struct addrinfo *list = NULL;
  int rc = getaddrinfo((host == NULL || host[0] == 0) ? NULL : host, service,
                       &hints, &list);
  if (rc != 0) {
#if defined(_WIN32)
    zyxnet_fail(ZYXNET_INVALID, rc);
#else
    if (rc == EAI_SYSTEM) {
      zyxnet_fail_from(errno);
    } else if (rc == EAI_AGAIN) {
      zyxnet_fail(ZYXNET_TIMEOUT, 0);
    } else {
      zyxnet_fail(ZYXNET_INVALID, 0);
    }
#endif
    return -1;
  }
  zyxnet_addrlen len = (zyxnet_addrlen)list->ai_addrlen;
  if ((size_t)len > sizeof(*out)) len = (zyxnet_addrlen)sizeof(*out);
  memcpy(out, list->ai_addr, (size_t)len);
  *out_len = len;
  freeaddrinfo(list);
  return 0;
}

void *zyxnet_listen(const char *network, size_t network_len, const char *host,
                    size_t host_len, int32_t port) {
  zyxnet_clear();
  zyxnet_start();
  int socktype, family, unix_domain;
  if (zyxnet_network(network, network_len, &socktype, &family, &unix_domain) != 0) {
    zyxnet_fail(ZYXNET_INVALID, 0);
    return NULL;
  }
  char *name = zyxnet_ncopy(host, host_len);
  if (name == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return NULL;
  }
  struct sockaddr_storage addr;
  zyxnet_addrlen len = 0;
  int ready = 0;
#if defined(AF_UNIX)
  if (unix_domain) {
    ready = zyxnet_unix_addr(name, &addr, &len) == 0;
  } else {
    ready = zyxnet_internet_addr(name, family, socktype, port, 1, &addr, &len) == 0;
  }
#else
  ready = zyxnet_internet_addr(name, family, socktype, port, 1, &addr, &len) == 0;
#endif
  free(name);
  if (!ready) return NULL;
  zyxnet_descriptor descriptor = socket(addr.ss_family, socktype, 0);
  if (descriptor == ZYXNET_BAD) {
    zyxnet_fail_from(zyxnet_last_error());
    return NULL;
  }
#if !defined(_WIN32)
  /* A listener that is closed and made again should not have to wait for the
     connections it carried to leave. Windows reads the same option as leave
     to a second socket to bind a port another holds, which would let two
     listeners answer for one port, so there the port is bound exclusively,
     which is what Windows does by default. */
  int on = 1;
  setsockopt(descriptor, SOL_SOCKET, SO_REUSEADDR, (const char *)&on, sizeof(on));
#endif
  if (bind(descriptor, (struct sockaddr *)&addr, len) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  if (listen(descriptor, 128) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  if (zyxnet_nonblocking(descriptor, 0) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  return zyxnet_wrap(descriptor, addr.ss_family, ZYXNET_LISTENER);
}

void *zyxnet_accept(void *handle) {
  zyxnet_clear();
  zyxnet_socket *listener = (zyxnet_socket *)handle;
  if (listener == NULL || listener->kind != ZYXNET_LISTENER) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return NULL;
  }
  struct sockaddr_storage peer;
  zyxnet_addrlen len = (zyxnet_addrlen)sizeof(peer);
  zyxnet_descriptor descriptor =
      accept(listener->descriptor, (struct sockaddr *)&peer, &len);
  if (descriptor == ZYXNET_BAD) {
    zyxnet_fail_from(zyxnet_last_error());
    return NULL;
  }
  if (zyxnet_nonblocking(descriptor, 0) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  return zyxnet_wrap(descriptor, peer.ss_family, ZYXNET_STREAM);
}

void *zyxnet_dial(const char *network, size_t network_len, const char *host,
                  size_t host_len, int32_t port) {
  zyxnet_clear();
  zyxnet_start();
  int socktype, family, unix_domain;
  if (zyxnet_network(network, network_len, &socktype, &family, &unix_domain) != 0) {
    zyxnet_fail(ZYXNET_INVALID, 0);
    return NULL;
  }
  char *name = zyxnet_ncopy(host, host_len);
  if (name == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return NULL;
  }
  struct sockaddr_storage addr;
  zyxnet_addrlen len = 0;
  int ready = 0;
#if defined(AF_UNIX)
  if (unix_domain) {
    ready = zyxnet_unix_addr(name, &addr, &len) == 0;
  } else {
    ready = zyxnet_internet_addr(name, family, socktype, port, 0, &addr, &len) == 0;
  }
#else
  ready = zyxnet_internet_addr(name, family, socktype, port, 0, &addr, &len) == 0;
#endif
  free(name);
  if (!ready) return NULL;
  zyxnet_descriptor descriptor = socket(addr.ss_family, socktype, 0);
  if (descriptor == ZYXNET_BAD) {
    zyxnet_fail_from(zyxnet_last_error());
    return NULL;
  }
  if (zyxnet_nonblocking(descriptor, 0) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  /* A connection to a machine that answers at once is up when connect
     returns; one that has to travel answers as being in progress, which the
     caller waits for by watching the descriptor. */
  int connected = connect(descriptor, (struct sockaddr *)&addr, len);
  if (connected != 0) {
    int error = zyxnet_last_error();
    if (!zyxnet_in_progress(error)) {
      zyxnet_fail_from(error);
      ZYXNET_CLOSE(descriptor);
      return NULL;
    }
  }
  void *handle = zyxnet_wrap(descriptor, addr.ss_family, ZYXNET_STREAM);
  if (handle != NULL && connected != 0) {
    ((zyxnet_socket *)handle)->pending = 1;
  }
  return handle;
}

int32_t zyxnet_connect_pending(void *handle) {
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) return 0;
  return socket->pending;
}

int32_t zyxnet_connect_state(void *handle) {
  /* The caller has waited for the descriptor to become writable, so the
     socket's own error says whether the connection came up: the system sets it
     when a connection that was in progress fails, and leaves it clear when the
     connection is up. */
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  int error = 0;
  zyxnet_addrlen len = (zyxnet_addrlen)sizeof(error);
  if (getsockopt(socket->descriptor, SOL_SOCKET, SO_ERROR, (char *)&error, &len) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  if (error != 0) {
    zyxnet_fail_from(error);
    return -1;
  }
  return 0;
}

void *zyxnet_bind_udp(const char *host, size_t host_len, int32_t port) {
  zyxnet_clear();
  zyxnet_start();
  char *name = zyxnet_ncopy(host, host_len);
  if (name == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return NULL;
  }
  struct sockaddr_storage addr;
  zyxnet_addrlen len = 0;
  int ready = zyxnet_internet_addr(name, AF_UNSPEC, SOCK_DGRAM, port, 1, &addr, &len) == 0;
  free(name);
  if (!ready) return NULL;
  zyxnet_descriptor descriptor = socket(addr.ss_family, SOCK_DGRAM, 0);
  if (descriptor == ZYXNET_BAD) {
    zyxnet_fail_from(zyxnet_last_error());
    return NULL;
  }
  if (bind(descriptor, (struct sockaddr *)&addr, len) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  if (zyxnet_nonblocking(descriptor, 0) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    ZYXNET_CLOSE(descriptor);
    return NULL;
  }
  return zyxnet_wrap(descriptor, addr.ss_family, ZYXNET_DATAGRAM);
}

int32_t zyxnet_fd(void *handle) {
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) return -1;
  return (int32_t)(intptr_t)socket->descriptor;
}

void zyxnet_close(void *handle) {
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) return;
  ZYXNET_CLOSE(socket->descriptor);
  free(socket);
}

int32_t zyxnet_shutdown(void *handle, int32_t how) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  int direction = ZYXNET_SHUT_BOTH;
  if (how == 0) {
    direction = ZYXNET_SHUT_READ;
  } else if (how == 1) {
    direction = ZYXNET_SHUT_WRITE;
  }
  if (shutdown(socket->descriptor, direction) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  return 0;
}

/* zyxnet_format_addr writes the text of an address, which is host:port, the
   bracketed form of an internet address of the newer family, or the path of a
   unix socket. */
static void zyxnet_format_addr(const struct sockaddr *addr, char *out,
                               size_t cap) {
  if (addr->sa_family == AF_INET) {
    const struct sockaddr_in *in = (const struct sockaddr_in *)addr;
    char ip[INET_ADDRSTRLEN];
    if (inet_ntop(AF_INET, &in->sin_addr, ip, sizeof(ip)) == NULL) {
      snprintf(out, cap, "?");
      return;
    }
    snprintf(out, cap, "%s:%u", ip, (unsigned)ntohs(in->sin_port));
    return;
  }
  if (addr->sa_family == AF_INET6) {
    const struct sockaddr_in6 *in6 = (const struct sockaddr_in6 *)addr;
    char ip[INET6_ADDRSTRLEN];
    if (inet_ntop(AF_INET6, &in6->sin6_addr, ip, sizeof(ip)) == NULL) {
      snprintf(out, cap, "?");
      return;
    }
    snprintf(out, cap, "[%s]:%u", ip, (unsigned)ntohs(in6->sin6_port));
    return;
  }
#if defined(AF_UNIX)
  if (addr->sa_family == AF_UNIX) {
    const struct sockaddr_un *un = (const struct sockaddr_un *)addr;
    snprintf(out, cap, "%s", un->sun_path);
    return;
  }
#endif
  snprintf(out, cap, "?");
}

/* zyxnet_name appends the text of an address to a buffer. */
static int zyxnet_name(zyxnet_descriptor descriptor, int peer, void *buf) {
  struct sockaddr_storage addr;
  zyxnet_addrlen len = (zyxnet_addrlen)sizeof(addr);
  int rc = peer ? getpeername(descriptor, (struct sockaddr *)&addr, &len)
                : getsockname(descriptor, (struct sockaddr *)&addr, &len);
  if (rc != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  char text[128];
  zyxnet_format_addr((const struct sockaddr *)&addr, text, sizeof(text));
  zyxrt_buf_write(buf, (const unsigned char *)text, strlen(text));
  return 0;
}

int32_t zyxnet_local_addr(void *handle, void *buf) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  return zyxnet_name(socket->descriptor, 0, buf);
}

int32_t zyxnet_remote_addr(void *handle, void *buf) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  return zyxnet_name(socket->descriptor, 1, buf);
}

/* zyxnet_take moves at most cap bytes from a descriptor into the language's
   own byte buffer. What a datagram read learned about the far end is left in
   the address buffer. */
static int64_t zyxnet_take(zyxnet_socket *socket, void *buf, size_t cap,
                           void *addr, int from) {
  if (cap == 0) return 0;
  if (cap > 65536) cap = 65536;
  unsigned char *scratch = (unsigned char *)malloc(cap);
  if (scratch == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return -1;
  }
  struct sockaddr_storage source;
  zyxnet_addrlen source_len = (zyxnet_addrlen)sizeof(source);
  int got = 0;
  for (;;) {
    int taken;
    if (from) {
      taken = (int)recvfrom(socket->descriptor, (char *)scratch, (int)cap, 0,
                            (struct sockaddr *)&source, &source_len);
    } else {
      taken = (int)recv(socket->descriptor, (char *)scratch, (int)cap, 0);
    }
    if (taken >= 0) {
      got = taken;
      break;
    }
    int error = zyxnet_last_error();
    if (error != ZYXNET_INTERRUPTED_NUMBER) {
      zyxnet_fail_from(error);
      free(scratch);
      return -1;
    }
    /* A read the system interrupted before it moved anything is asked for
       again. */
  }
  if (got > 0) zyxrt_buf_write(buf, scratch, (size_t)got);
  if (from && addr != NULL && got >= 0) {
    char text[128];
    zyxnet_format_addr((const struct sockaddr *)&source, text, sizeof(text));
    zyxrt_buf_write(addr, (const unsigned char *)text, strlen(text));
  }
  free(scratch);
  return got;
}

int64_t zyxnet_recv(void *handle, void *buf, size_t cap) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  return zyxnet_take(socket, buf, cap, NULL, 0);
}

int64_t zyxnet_recv_from(void *handle, void *buf, size_t cap, void *addr) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  return zyxnet_take(socket, buf, cap, addr, 1);
}

int64_t zyxnet_send(void *handle, const unsigned char *buf, size_t len) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  if (len == 0) return 0;
  if (len > 65536) len = 65536;
  for (;;) {
    int sent = (int)send(socket->descriptor, (const char *)buf, (int)len, 0);
    if (sent >= 0) return sent;
    int error = zyxnet_last_error();
    if (error != ZYXNET_INTERRUPTED_NUMBER) {
      zyxnet_fail_from(error);
      return -1;
    }
  }
}

int64_t zyxnet_send_to(void *handle, const unsigned char *buf, size_t len,
                       const char *host, size_t host_len, int32_t port) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  char *name = zyxnet_ncopy(host, host_len);
  if (name == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return -1;
  }
  struct sockaddr_storage addr;
  zyxnet_addrlen addr_len = 0;
  int ready =
      zyxnet_internet_addr(name, socket->family, SOCK_DGRAM, port, 0, &addr, &addr_len) == 0;
  free(name);
  if (!ready) return -1;
  if (len > 65536) len = 65536;
  int sent = (int)sendto(socket->descriptor, (const char *)buf, (int)len, 0,
                         (struct sockaddr *)&addr, addr_len);
  if (sent < 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  return sent;
}

int32_t zyxnet_set_blocking(void *handle, int32_t on) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  if (zyxnet_nonblocking(socket->descriptor, on != 0) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  return 0;
}

int32_t zyxnet_set_no_delay(void *handle, int32_t on) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  int value = on != 0;
  if (setsockopt(socket->descriptor, IPPROTO_TCP, TCP_NODELAY, (const char *)&value,
                 sizeof(value)) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  return 0;
}

int32_t zyxnet_set_keep_alive(void *handle, int32_t on) {
  zyxnet_clear();
  zyxnet_socket *socket = (zyxnet_socket *)handle;
  if (socket == NULL) {
    zyxnet_fail(ZYXNET_CLOSED, 0);
    return -1;
  }
  int value = on != 0;
  if (setsockopt(socket->descriptor, SOL_SOCKET, SO_KEEPALIVE, (const char *)&value,
                 sizeof(value)) != 0) {
    zyxnet_fail_from(zyxnet_last_error());
    return -1;
  }
  return 0;
}

int32_t zyxnet_resolve(const char *host, size_t host_len, void *buf) {
  zyxnet_clear();
  zyxnet_start();
  char *name = zyxnet_ncopy(host, host_len);
  if (name == NULL) {
    zyxnet_fail(ZYXNET_OTHER, 0);
    return -1;
  }
  struct addrinfo hints;
  memset(&hints, 0, sizeof(hints));
  hints.ai_family = AF_UNSPEC;
  hints.ai_socktype = SOCK_STREAM;
  hints.ai_flags = AI_NUMERICSERV;
  struct addrinfo *list = NULL;
  int rc = getaddrinfo(name, NULL, &hints, &list);
  free(name);
  if (rc != 0) {
    if (rc == EAI_NONAME) {
      zyxnet_fail(ZYXNET_NOT_FOUND, 0);
    } else if (rc == EAI_AGAIN) {
      zyxnet_fail(ZYXNET_TIMEOUT, 0);
    } else {
#if defined(_WIN32)
      zyxnet_fail(ZYXNET_INVALID, rc);
#else
      if (rc == EAI_SYSTEM) {
        zyxnet_fail_from(errno);
      } else {
        zyxnet_fail(ZYXNET_INVALID, 0);
      }
#endif
    }
    return -1;
  }
  /* A name may answer with the same address for each socket type, so what is
     already written is compared before another is added. */
  char seen[16][INET6_ADDRSTRLEN];
  int count = 0;
  for (struct addrinfo *entry = list; entry != NULL; entry = entry->ai_next) {
    if (entry->ai_family != AF_INET && entry->ai_family != AF_INET6) continue;
    const void *source =
        entry->ai_family == AF_INET
            ? (const void *)&((const struct sockaddr_in *)entry->ai_addr)->sin_addr
            : (const void *)&((const struct sockaddr_in6 *)entry->ai_addr)->sin6_addr;
    char text[INET6_ADDRSTRLEN];
    if (inet_ntop(entry->ai_family, source, text, sizeof(text)) == NULL) continue;
    int known = 0;
    for (int i = 0; i < count; i++) {
      if (strcmp(seen[i], text) == 0) {
        known = 1;
        break;
      }
    }
    if (known) continue;
    if (count < 16) {
      snprintf(seen[count], sizeof(seen[count]), "%s", text);
      count++;
    }
    zyxrt_buf_write(buf, (const unsigned char *)text, strlen(text));
    unsigned char zero = 0;
    zyxrt_buf_write(buf, &zero, 1);
  }
  freeaddrinfo(list);
  return 0;
}
