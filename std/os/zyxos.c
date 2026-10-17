#include "zyxos.h"

#include "zyxrt.h"

/* The C compiler is asked for strict ISO C, under which a system hides the
   declarations this file is written against. The word has to come before any
   system header. */
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

#include <process.h>
#include <winsock2.h>

#else

#include <unistd.h>

#endif

#if defined(__APPLE__)

#include <crt_externs.h>

#endif

/* The failure state of the calling thread. */
static _Thread_local int32_t zyxos_kind;
static _Thread_local int32_t zyxos_errno;
static _Thread_local char zyxos_text[256];

static void zyxos_fail(int32_t kind, int number) {
  zyxos_kind = kind;
  zyxos_errno = number;
  const char *text = strerror(number);
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  if (n >= sizeof(zyxos_text)) n = sizeof(zyxos_text) - 1;
  memcpy(zyxos_text, text, n);
  zyxos_text[n] = 0;
}

static void zyxos_clear(void) {
  zyxos_kind = 0;
  zyxos_errno = 0;
  zyxos_text[0] = 0;
}

int32_t zyxos_error_kind(void) { return zyxos_kind; }

int32_t zyxos_error_number(void) { return zyxos_errno; }

void zyxos_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxos_text, strlen(zyxos_text));
}

/* zyxos_append writes a text and the zero byte that ends it, which is what a
   list of names read back one at a time is made of. */
static void zyxos_append(void *buf, const char *text) {
  zyxrt_buf_write(buf, (const unsigned char *)text, strlen(text));
  unsigned char zero = 0;
  zyxrt_buf_write(buf, &zero, 1);
}

/* cvalue copies the bytes of a name or a value into a terminated string,
   because the platform's calls want one and a str of the language is a view
   without a terminator. */
static char *cvalue(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, text, len);
  out[len] = 0;
  return out;
}

/* The arguments of the process. The system keeps them in a different place on
   each of the three families of targets, so each one is written out. */
#if defined(_WIN32)

int32_t zyxos_args(void *buf) {
  zyxos_clear();
  for (int i = 0; i < __argc; i++) {
    zyxos_append(buf, __argv[i]);
  }
  return 0;
}

#elif defined(__APPLE__)

int32_t zyxos_args(void *buf) {
  zyxos_clear();
  int count = *_NSGetArgc();
  char **values = *_NSGetArgv();
  for (int i = 0; i < count; i++) {
    zyxos_append(buf, values[i]);
  }
  return 0;
}

#elif defined(__linux__) || defined(__ANDROID__) || defined(__OHOS__)

int32_t zyxos_args(void *buf) {
  zyxos_clear();
  /* The kernel keeps the arguments of a process in one file, each ended by a
     zero byte, so the whole of it is what the language reads back. */
  FILE *file = fopen("/proc/self/cmdline", "rb");
  if (file == NULL) {
    zyxos_fail(ZYXOS_OTHER, errno);
    return -1;
  }
  unsigned char chunk[4096];
  size_t n;
  while ((n = fread(chunk, 1, sizeof(chunk), file)) > 0) {
    zyxrt_buf_write(buf, chunk, n);
  }
  fclose(file);
  return 0;
}

#else

int32_t zyxos_args(void *buf) {
  zyxos_clear();
  /* A target with no process to speak of has no arguments. */
  (void)buf;
  return 0;
}

#endif

int32_t zyxos_env(void *buf, const char *name, size_t len) {
  zyxos_clear();
  char *key = cvalue(name, len);
  if (key == NULL) {
    zyxos_fail(ZYXOS_OTHER, 0);
    return -1;
  }
  const char *value = getenv(key);
  free(key);
  if (value == NULL) {
    zyxos_fail(ZYXOS_NOT_FOUND, 0);
    return -1;
  }
  zyxrt_buf_write(buf, (const unsigned char *)value, strlen(value));
  return 0;
}

int32_t zyxos_env_all(void *buf) {
  zyxos_clear();
#if defined(_WIN32)
  /* The C runtime of Windows keeps the environment in one string, with the
     variables separated by a zero byte and the whole of it ended by two. */
  char *whole = GetEnvironmentStringsA();
  if (whole == NULL) {
    zyxos_fail(ZYXOS_OTHER, 0);
    return -1;
  }
  for (char *at = whole; *at != 0; at += strlen(at) + 1) {
    /* A variable a command shell defines for itself begins with an equal
       sign and is not part of the environment a program reads. */
    if (*at == '=') continue;
    char *split = strchr(at, '=');
    if (split == NULL) continue;
    *split = 0;
    zyxos_append(buf, at);
    zyxos_append(buf, split + 1);
    *split = '=';
  }
  FreeEnvironmentStringsA(whole);
  return 0;
#else
  extern char **environ;
  if (environ == NULL) return 0;
  for (char **at = environ; *at != NULL; at++) {
    char *split = strchr(*at, '=');
    if (split == NULL) continue;
    size_t name_len = (size_t)(split - *at);
    if (name_len == 0) continue;
    char *name = cvalue(*at, name_len);
    if (name == NULL) {
      zyxos_fail(ZYXOS_OTHER, 0);
      return -1;
    }
    zyxos_append(buf, name);
    zyxos_append(buf, split + 1);
    free(name);
  }
  return 0;
#endif
}

int32_t zyxos_setenv(const char *name, size_t name_len, const char *value, size_t value_len) {
  zyxos_clear();
  char *key = cvalue(name, name_len);
  char *text = cvalue(value, value_len);
  if (key == NULL || text == NULL) {
    free(key);
    free(text);
    zyxos_fail(ZYXOS_OTHER, 0);
    return -1;
  }
#if defined(_WIN32)
  int rc = _putenv_s(key, text);
#else
  int rc = setenv(key, text, 1);
#endif
  free(key);
  free(text);
  if (rc != 0) {
    zyxos_fail(ZYXOS_OTHER, errno);
    return -1;
  }
  return 0;
}

int32_t zyxos_unsetenv(const char *name, size_t len) {
  zyxos_clear();
  char *key = cvalue(name, len);
  if (key == NULL) {
    zyxos_fail(ZYXOS_OTHER, 0);
    return -1;
  }
#if defined(_WIN32)
  int rc = _putenv_s(key, "");
#else
  int rc = unsetenv(key);
#endif
  free(key);
  if (rc != 0) {
    zyxos_fail(ZYXOS_OTHER, errno);
    return -1;
  }
  return 0;
}

#if defined(_WIN32)
int32_t zyxos_getpid(void) { return (int32_t)_getpid(); }
#else
int32_t zyxos_getpid(void) { return (int32_t)getpid(); }
#endif

/* The handle of a stream the process begins with is the file package's own,
   which this package reaches by the name that package links: a program that
   imports this one imports that one too. */
void *zyxfs_standard(int32_t which);

void *zyxos_standard(int32_t which) { return zyxfs_standard(which); }

/* The two segments of the target. Each is the name the platform matrix uses
   for the platform the code was built for, which is what the compiler's own
   target table says of it. */
void zyxos_os(void *buf) {
  const char *name = "unknown";
#if defined(_WIN32)
  name = "windows";
#elif defined(__APPLE__)
  name = "darwin";
#elif defined(__ANDROID__)
  name = "android";
#elif defined(__OHOS__)
  name = "harmony";
#elif defined(__linux__)
  name = "linux";
#elif defined(__wasm__)
  name = "wasm";
#endif
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
}

void zyxos_arch(void *buf) {
  const char *name = "unknown";
#if defined(__x86_64__) || defined(_M_X64)
  name = "amd64";
#elif defined(__aarch64__) || defined(_M_ARM64)
  name = "arm64";
#elif defined(__i386__) || defined(_M_IX86)
  name = "i386";
#elif defined(__arm__)
  name = "arm";
#elif defined(__wasm32__)
  name = "wasm32";
#endif
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
}

int32_t zyxos_hostname(void *buf) {
  zyxos_clear();
  char name[256];
  if (gethostname(name, sizeof(name)) != 0) {
    zyxos_fail(ZYXOS_OTHER, errno);
    return -1;
  }
  name[sizeof(name) - 1] = 0;
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
  return 0;
}

void zyxos_exit(int32_t code) { exit(code); }
