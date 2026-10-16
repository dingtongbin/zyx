/* Event loop backend: Winsock readiness. Windows has no readiness facility
   for handles in general, so the loop watches sockets through WSAPoll, which
   takes the set of descriptors with every call: the backend reads the
   registered set each wait, and a loopback socket pair gives the scheduler a
   way to make the poller look again. A pipe, a console, and an ordinary file
   are not sockets, so those go through the blocking pool. */

#include "zyxrt_internal.h"

#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0600
#endif

#include <string.h>
#include <winsock2.h>
#include <ws2tcpip.h>

static SOCKET wake_read = INVALID_SOCKET;
static SOCKET wake_write = INVALID_SOCKET;

/* wake_pair builds the loopback pair the scheduler writes to and the poller
   watches, both non-blocking. */
static int wake_pair(void) {
  SOCKET listener = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
  if (listener == INVALID_SOCKET) return -1;
  struct sockaddr_in addr;
  memset(&addr, 0, sizeof(addr));
  addr.sin_family = AF_INET;
  addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  addr.sin_port = 0;
  if (bind(listener, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
    closesocket(listener);
    return -1;
  }
  if (listen(listener, 1) != 0) {
    closesocket(listener);
    return -1;
  }
  int len = (int)sizeof(addr);
  if (getsockname(listener, (struct sockaddr *)&addr, &len) != 0) {
    closesocket(listener);
    return -1;
  }
  SOCKET client = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
  if (client == INVALID_SOCKET) {
    closesocket(listener);
    return -1;
  }
  if (connect(client, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
    closesocket(client);
    closesocket(listener);
    return -1;
  }
  SOCKET server = accept(listener, NULL, NULL);
  closesocket(listener);
  if (server == INVALID_SOCKET) {
    closesocket(client);
    return -1;
  }
  u_long nonblocking = 1;
  ioctlsocket(client, FIONBIO, &nonblocking);
  ioctlsocket(server, FIONBIO, &nonblocking);
  wake_write = client;
  wake_read = server;
  return 0;
}

int zyxrt_loop_backend_init(void) {
  WSADATA data;
  if (WSAStartup(MAKEWORD(2, 2), &data) != 0) return -1;
  if (wake_pair() != 0) return -1;
  return 0;
}

int zyxrt_loop_backend_add(int fd, int events) {
  /* WSAPoll reads the set every wait, so the registration is the loop's own
     table and there is nothing to tell the kernel. */
  (void)fd;
  (void)events;
  return 0;
}

int zyxrt_loop_backend_wait(int *fds, int *events, int max, int timeout_ms) {
  if (wake_read == INVALID_SOCKET) return -1;
  int snap_fds[ZYXRT_LOOP_BATCH];
  int snap_events[ZYXRT_LOOP_BATCH];
  size_t cap = ZYXRT_LOOP_BATCH - 1;
  if (cap > (size_t)max) cap = (size_t)max;
  size_t n = zyxrt_loop_snapshot(snap_fds, snap_events, cap);

  WSAPOLLFD set[ZYXRT_LOOP_BATCH];
  for (size_t i = 0; i < n; i++) {
    set[i].fd = (SOCKET)snap_fds[i];
    set[i].events = 0;
    if (snap_events[i] & ZYXRT_FD_READ) set[i].events |= POLLRDNORM;
    if (snap_events[i] & ZYXRT_FD_WRITE) set[i].events |= POLLWRNORM;
    set[i].revents = 0;
  }
  set[n].fd = wake_read;
  set[n].events = POLLRDNORM;
  set[n].revents = 0;
  n++;

  int got = WSAPoll(set, (ULONG)n, timeout_ms);
  if (got == SOCKET_ERROR) return -1;
  if (got == 0) return 0;

  int out = 0;
  for (size_t i = 0; i + 1 < n; i++) {
    if (set[i].revents == 0) continue;
    int ev = 0;
    if (set[i].revents & (POLLRDNORM | POLLHUP | POLLERR | POLLNVAL)) ev |= ZYXRT_FD_READ;
    if (set[i].revents & POLLWRNORM) ev |= ZYXRT_FD_WRITE;
    if (ev == 0) continue;
    fds[out] = snap_fds[i];
    events[out] = ev;
    out++;
  }
  if (set[n - 1].revents != 0) {
    /* The wakeup socket: drain what the signals left, up to a bound so the
       poller cannot be held here by a flood. */
    char buf[64];
    for (int i = 0; i < 64; i++) {
      if (recv(wake_read, buf, (int)sizeof(buf), 0) <= 0) break;
    }
  }
  return out;
}

void zyxrt_loop_backend_wake(void) {
  if (wake_write == INVALID_SOCKET) return;
  const char one = 1;
  int wrote = send(wake_write, &one, 1, 0);
  (void)wrote;
}
