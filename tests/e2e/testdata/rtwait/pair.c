#include "pair.h"

#if defined(_WIN32)

#include <stdint.h>
#include <string.h>
#include <winsock2.h>

int pair_open(int *a, int *b) {
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
  *a = (int)(intptr_t)server;
  *b = (int)(intptr_t)client;
  return 0;
}

int pair_write(int fd, int byte) {
  char one = (char)byte;
  return (int)send((SOCKET)(intptr_t)fd, &one, 1, 0);
}

int pair_read(int fd) {
  char one = 0;
  return (int)recv((SOCKET)(intptr_t)fd, &one, 1, 0);
}

int pair_close(int fd) { return closesocket((SOCKET)(intptr_t)fd); }

#else

#include <sys/socket.h>
#include <unistd.h>

int pair_open(int *a, int *b) {
  int fds[2];
  if (socketpair(AF_UNIX, SOCK_STREAM, 0, fds) != 0) return -1;
  *a = fds[0];
  *b = fds[1];
  return 0;
}

int pair_write(int fd, int byte) {
  char one = (char)byte;
  return (int)write(fd, &one, 1);
}

int pair_read(int fd) {
  char one = 0;
  return (int)read(fd, &one, 1);
}

int pair_close(int fd) { return close(fd); }

#endif
