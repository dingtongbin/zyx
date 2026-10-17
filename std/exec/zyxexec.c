#include "zyxexec.h"

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

#include <fcntl.h>
#include <io.h>
#include <windows.h>

#else

#include <signal.h>
#include <sys/wait.h>
#include <unistd.h>

#endif

/* The failure state of the calling thread, in the shape every system package
   reports one: what went wrong, the platform's own number, and its text. */
static _Thread_local int32_t zyxexec_kind;
static _Thread_local int32_t zyxexec_errno;
static _Thread_local char zyxexec_text[256];

static void zyxexec_set(int32_t kind, int32_t number, const char *text) {
  zyxexec_kind = kind;
  zyxexec_errno = number;
  size_t n = text == NULL ? 0 : strlen(text);
  if (n >= sizeof(zyxexec_text)) n = sizeof(zyxexec_text) - 1;
  if (n > 0) memcpy(zyxexec_text, text, n);
  zyxexec_text[n] = 0;
}

static void zyxexec_clear(void) {
  zyxexec_kind = 0;
  zyxexec_errno = 0;
  zyxexec_text[0] = 0;
}

int32_t zyxexec_error_kind(void) { return zyxexec_kind; }

int32_t zyxexec_error_number(void) { return zyxexec_errno; }

void zyxexec_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxexec_text, strlen(zyxexec_text));
}

#if !defined(_WIN32)

/* the platform's own error numbers mapped onto the shared shape. The numbers
   are the platform's, so the mapping is written where they are. */
static int32_t zyxexec_kind_of(int number) {
  switch (number) {
    case ENOENT:
      return ZYXEXEC_NOT_FOUND;
    case EACCES:
    case EPERM:
      return ZYXEXEC_PERMISSION;
    case EEXIST:
      return ZYXEXEC_EXISTS;
    case ENOTDIR:
      return ZYXEXEC_NOT_DIR;
    case EISDIR:
      return ZYXEXEC_IS_DIR;
    case EINVAL:
      return ZYXEXEC_INVALID;
    case EBADF:
      return ZYXEXEC_CLOSED;
    case EINTR:
      return ZYXEXEC_INTERRUPTED;
#if defined(EWOULDBLOCK) && EWOULDBLOCK != EAGAIN
    case EWOULDBLOCK:
#endif
    case EAGAIN:
      return ZYXEXEC_WOULD_BLOCK;
    default:
      return ZYXEXEC_OTHER;
  }
}

/* zyxexec_failed records the failure the platform just reported. */
static void zyxexec_failed(void) {
  zyxexec_set(zyxexec_kind_of(errno), errno, strerror(errno));
}

#else

/* zyxexec_kind_of maps the failure numbers of the platform onto the shared
   shape. */
static int32_t zyxexec_kind_of(DWORD number) {
  switch (number) {
    case ERROR_FILE_NOT_FOUND:
    case ERROR_PATH_NOT_FOUND:
      return ZYXEXEC_NOT_FOUND;
    case ERROR_ACCESS_DENIED:
      return ZYXEXEC_PERMISSION;
    case ERROR_ALREADY_EXISTS:
    case ERROR_FILE_EXISTS:
      return ZYXEXEC_EXISTS;
    case ERROR_INVALID_PARAMETER:
    case ERROR_BAD_ARGUMENTS:
      return ZYXEXEC_INVALID;
    case ERROR_BROKEN_PIPE:
    case ERROR_NO_DATA:
      return ZYXEXEC_CLOSED;
    default:
      return ZYXEXEC_OTHER;
  }
}

/* zyxexec_failed records the failure the platform just reported, with the text
   the system gives it. */
static void zyxexec_failed(void) {
  DWORD code = GetLastError();
  char *text = NULL;
  DWORD n = FormatMessageA(FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM |
                               FORMAT_MESSAGE_IGNORE_INSERTS | FORMAT_MESSAGE_MAX_WIDTH_MASK,
                           NULL, code, 0, (LPSTR)&text, 0, NULL);
  if (text != NULL) {
    /* The system text ends with the line breaks the message itself carries. */
    size_t len = n;
    while (len > 0 && (text[len - 1] == '\r' || text[len - 1] == '\n' || text[len - 1] == ' ')) {
      len--;
    }
    text[len] = 0;
    zyxexec_set(zyxexec_kind_of(code), (int32_t)code, text);
    LocalFree(text);
  } else {
    zyxexec_set(zyxexec_kind_of(code), (int32_t)code, "the process call failed");
  }
}

#endif

/* The layout of the handle the file package reads: a descriptor and whether
   that package owns it. The pipes below are given to that package, so the
   handle has to be the shape it casts a handle to. */
typedef struct {
  int fd;
  int owned;
} zyxexec_file;

/* A child: the process, its number, and, when its streams are pipes, one
   handle per stream for the end this process holds. */
typedef struct {
  int32_t pid;
  void *in;
  void *out;
  void *err;
#if defined(_WIN32)
  HANDLE process;
#else
  /* On a POSIX target the child's number is what the calls take. */
  int32_t reserved;
#endif
} zyxexec_child;

/* zyxexec_file_new builds one file handle over a descriptor, in the shape the
   file package owns and frees. */
static void *zyxexec_file_new(int fd) {
  zyxexec_file *file = (zyxexec_file *)malloc(sizeof(zyxexec_file));
  if (file == NULL) return NULL;
  file->fd = fd;
  file->owned = 1;
  return file;
}

/* zyxexec_args walks the arguments of a call, each ended by a zero byte, and
   answers the one at an offset together with the offset after it. */
static const char *zyxexec_arg_at(const char *args, size_t args_len, size_t *at, size_t *len) {
  if (*at >= args_len) return NULL;
  const char *start = args + *at;
  size_t n = 0;
  while (*at + n < args_len && start[n] != 0) n++;
  *len = n;
  *at += n + 1;
  return start;
}

#if defined(_WIN32)

/* zyxexec_append copies a text into a growing buffer. */
typedef struct {
  char *data;
  size_t len;
  size_t cap;
} zyxexec_text_buf;

static int zyxexec_append(zyxexec_text_buf *out, const char *s, size_t n) {
  if (out->len + n + 1 > out->cap) {
    size_t cap = out->cap == 0 ? 64 : out->cap;
    while (cap < out->len + n + 1) cap *= 2;
    char *grown = (char *)realloc(out->data, cap);
    if (grown == NULL) return -1;
    out->data = grown;
    out->cap = cap;
  }
  memcpy(out->data + out->len, s, n);
  out->len += n;
  out->data[out->len] = 0;
  return 0;
}

/* zyxexec_append_quoted adds one argument to a Windows command line, in the
   form the platform's own argument reader takes back: an argument with a
   space, a tab, or a quote is surrounded by quotes, and every backslash that
   a quote follows is doubled. */
static int zyxexec_append_quoted(zyxexec_text_buf *out, const char *s, size_t n) {
  /* An argument of no bytes still has to stand on the command line, so it is
     written as a pair of quotes. */
  if (n == 0) return zyxexec_append(out, "\"\"", 2);
  int plain = 1;
  for (size_t i = 0; i < n; i++) {
    if (s[i] == ' ' || s[i] == '\t' || s[i] == '"') {
      plain = 0;
      break;
    }
  }
  if (plain) return zyxexec_append(out, s, n);
  if (zyxexec_append(out, "\"", 1) != 0) return -1;
  size_t backslashes = 0;
  for (size_t i = 0; i < n; i++) {
    char ch = s[i];
    if (ch == '\\') {
      backslashes++;
      continue;
    }
    if (ch == '"') {
      for (size_t k = 0; k < backslashes * 2 + 1; k++) {
        if (zyxexec_append(out, "\\", 1) != 0) return -1;
      }
      if (zyxexec_append(out, "\"", 1) != 0) return -1;
      backslashes = 0;
      continue;
    }
    for (size_t k = 0; k < backslashes; k++) {
      if (zyxexec_append(out, "\\", 1) != 0) return -1;
    }
    backslashes = 0;
    if (zyxexec_append(out, &ch, 1) != 0) return -1;
  }
  for (size_t k = 0; k < backslashes; k++) {
    if (zyxexec_append(out, "\\", 1) != 0) return -1;
  }
  return zyxexec_append(out, "\"", 1);
}

/* zyxexec_command_line builds the one string the platform's process call
   takes: the program, and then each argument, with a space before each. */
static char *zyxexec_command_line(const char *cmd, size_t cmd_len, const char *args, size_t args_len) {
  zyxexec_text_buf out;
  out.data = NULL;
  out.len = 0;
  out.cap = 0;
  if (zyxexec_append_quoted(&out, cmd, cmd_len) != 0) {
    free(out.data);
    return NULL;
  }
  size_t at = 0;
  size_t len = 0;
  const char *arg;
  while ((arg = zyxexec_arg_at(args, args_len, &at, &len)) != NULL) {
    if (zyxexec_append(&out, " ", 1) != 0 || zyxexec_append_quoted(&out, arg, len) != 0) {
      free(out.data);
      return NULL;
    }
  }
  if (out.data == NULL) {
    out.data = (char *)malloc(1);
    if (out.data != NULL) out.data[0] = 0;
  }
  return out.data;
}

/* zyxexec_fd_from_handle hands a system handle to the C runtime, which is the
   descriptor a file handle of the file package reads. */
static int zyxexec_fd_from_handle(HANDLE h, int flags) {
  intptr_t fd = _open_osfhandle((intptr_t)h, flags);
  return (int)fd;
}

void *zyxexec_start(const char *cmd, size_t cmd_len, const char *args, size_t args_len, int32_t mode) {
  zyxexec_clear();
  HANDLE in_r = NULL, in_w = NULL, out_r = NULL, out_w = NULL, err_r = NULL, err_w = NULL;
  SECURITY_ATTRIBUTES sa;
  sa.nLength = (DWORD)sizeof(sa);
  sa.lpSecurityDescriptor = NULL;
  sa.bInheritHandle = TRUE;
  if (mode == ZYXEXEC_PIPE) {
    if (!CreatePipe(&in_r, &in_w, &sa, 0)) {
      zyxexec_failed();
      return NULL;
    }
    if (!CreatePipe(&out_r, &out_w, &sa, 0)) {
      zyxexec_failed();
      CloseHandle(in_r);
      CloseHandle(in_w);
      return NULL;
    }
    if (!CreatePipe(&err_r, &err_w, &sa, 0)) {
      zyxexec_failed();
      CloseHandle(in_r);
      CloseHandle(in_w);
      CloseHandle(out_r);
      CloseHandle(out_w);
      return NULL;
    }
    /* The ends this process keeps must not reach the child. */
    SetHandleInformation(in_w, HANDLE_FLAG_INHERIT, 0);
    SetHandleInformation(out_r, HANDLE_FLAG_INHERIT, 0);
    SetHandleInformation(err_r, HANDLE_FLAG_INHERIT, 0);
  }
  char *line = zyxexec_command_line(cmd, cmd_len, args, args_len);
  if (line == NULL) {
    zyxexec_set(ZYXEXEC_OTHER, 0, "out of memory");
    if (mode == ZYXEXEC_PIPE) {
      CloseHandle(in_r);
      CloseHandle(in_w);
      CloseHandle(out_r);
      CloseHandle(out_w);
      CloseHandle(err_r);
      CloseHandle(err_w);
    }
    return NULL;
  }
  STARTUPINFOA si;
  ZeroMemory(&si, sizeof(si));
  si.cb = (DWORD)sizeof(si);
  BOOL inherit = FALSE;
  if (mode == ZYXEXEC_PIPE) {
    si.dwFlags = STARTF_USESTDHANDLES;
    si.hStdInput = in_r;
    si.hStdOutput = out_w;
    si.hStdError = err_w;
    inherit = TRUE;
  }
  PROCESS_INFORMATION pi;
  ZeroMemory(&pi, sizeof(pi));
  if (!CreateProcessA(NULL, line, NULL, NULL, inherit, 0, NULL, NULL, &si, &pi)) {
    zyxexec_failed();
    free(line);
    if (mode == ZYXEXEC_PIPE) {
      CloseHandle(in_r);
      CloseHandle(in_w);
      CloseHandle(out_r);
      CloseHandle(out_w);
      CloseHandle(err_r);
      CloseHandle(err_w);
    }
    return NULL;
  }
  free(line);
  CloseHandle(pi.hThread);
  zyxexec_child *child = (zyxexec_child *)calloc(1, sizeof(zyxexec_child));
  if (child == NULL) {
    zyxexec_set(ZYXEXEC_OTHER, 0, "out of memory");
    TerminateProcess(pi.hProcess, 1);
    CloseHandle(pi.hProcess);
    if (mode == ZYXEXEC_PIPE) {
      CloseHandle(in_r);
      CloseHandle(in_w);
      CloseHandle(out_r);
      CloseHandle(out_w);
      CloseHandle(err_r);
      CloseHandle(err_w);
    }
    return NULL;
  }
  child->process = pi.hProcess;
  child->pid = (int32_t)pi.dwProcessId;
  if (mode == ZYXEXEC_PIPE) {
    /* The child holds the other ends now, so this process lets go of them. */
    CloseHandle(in_r);
    CloseHandle(out_w);
    CloseHandle(err_w);
    child->in = zyxexec_file_new(zyxexec_fd_from_handle(in_w, _O_WRONLY | _O_BINARY));
    child->out = zyxexec_file_new(zyxexec_fd_from_handle(out_r, _O_RDONLY | _O_BINARY));
    child->err = zyxexec_file_new(zyxexec_fd_from_handle(err_r, _O_RDONLY | _O_BINARY));
    if (child->in == NULL || child->out == NULL || child->err == NULL) {
      zyxexec_set(ZYXEXEC_OTHER, 0, "out of memory");
      TerminateProcess(child->process, 1);
      CloseHandle(child->process);
      free(child);
      return NULL;
    }
  }
  return child;
}

int32_t zyxexec_pid(void *handle) {
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL) return 0;
  return child->pid;
}

void *zyxexec_stream(void *handle, int32_t which) {
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL) return NULL;
  switch (which) {
    case 0:
      return child->in;
    case 1:
      return child->out;
    case 2:
      return child->err;
    default:
      return NULL;
  }
}

int32_t zyxexec_wait(void *handle, int32_t *code, int32_t *signal) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->process == NULL) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  DWORD result = WaitForSingleObject(child->process, INFINITE);
  if (result != WAIT_OBJECT_0) {
    zyxexec_failed();
    return -1;
  }
  DWORD status = 0;
  if (!GetExitCodeProcess(child->process, &status)) {
    zyxexec_failed();
    return -1;
  }
  CloseHandle(child->process);
  child->process = NULL;
  *code = (int32_t)status;
  *signal = 0;
  return 0;
}

int32_t zyxexec_poll(void *handle, int32_t *code, int32_t *signal) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->process == NULL) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  DWORD result = WaitForSingleObject(child->process, 0);
  if (result == WAIT_TIMEOUT) return 0;
  if (result != WAIT_OBJECT_0) {
    zyxexec_failed();
    return -1;
  }
  DWORD status = 0;
  if (!GetExitCodeProcess(child->process, &status)) {
    zyxexec_failed();
    return -1;
  }
  CloseHandle(child->process);
  child->process = NULL;
  *code = (int32_t)status;
  *signal = 0;
  return 1;
}

int32_t zyxexec_kill(void *handle) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->process == NULL) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  if (!TerminateProcess(child->process, 1)) {
    zyxexec_failed();
    return -1;
  }
  return 0;
}

void zyxexec_free(void *handle) {
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL) return;
  if (child->process != NULL) CloseHandle(child->process);
  free(child);
}

#else /* the POSIX family */

/* zyxexec_argv builds the terminated argument vector the exec call takes: the
   program first and then each argument. */
static char **zyxexec_argv(const char *cmd, size_t cmd_len, const char *args, size_t args_len) {
  size_t count = 0;
  size_t at = 0;
  size_t len = 0;
  while (zyxexec_arg_at(args, args_len, &at, &len) != NULL) count++;
  char **argv = (char **)calloc(count + 2, sizeof(char *));
  if (argv == NULL) return NULL;
  char *program = (char *)malloc(cmd_len + 1);
  if (program == NULL) {
    free(argv);
    return NULL;
  }
  memcpy(program, cmd, cmd_len);
  program[cmd_len] = 0;
  argv[0] = program;
  at = 0;
  for (size_t i = 1; i <= count; i++) {
    const char *arg = zyxexec_arg_at(args, args_len, &at, &len);
    char *copy = (char *)malloc(len + 1);
    if (copy == NULL) {
      for (size_t k = 0; k < i; k++) free(argv[k]);
      free(argv);
      return NULL;
    }
    memcpy(copy, arg, len);
    copy[len] = 0;
    argv[i] = copy;
  }
  return argv;
}

static void zyxexec_free_argv(char **argv) {
  if (argv == NULL) return;
  for (size_t i = 0; argv[i] != NULL; i++) free(argv[i]);
  free(argv);
}

void *zyxexec_start(const char *cmd, size_t cmd_len, const char *args, size_t args_len, int32_t mode) {
  zyxexec_clear();
  int in_pipe[2] = {-1, -1};
  int out_pipe[2] = {-1, -1};
  int err_pipe[2] = {-1, -1};
  if (mode == ZYXEXEC_PIPE) {
    if (pipe(in_pipe) != 0 || pipe(out_pipe) != 0 || pipe(err_pipe) != 0) {
      zyxexec_failed();
      for (int i = 0; i < 2; i++) {
        if (in_pipe[i] >= 0) close(in_pipe[i]);
        if (out_pipe[i] >= 0) close(out_pipe[i]);
        if (err_pipe[i] >= 0) close(err_pipe[i]);
      }
      return NULL;
    }
  }
  char **argv = zyxexec_argv(cmd, cmd_len, args, args_len);
  if (argv == NULL) {
    zyxexec_set(ZYXEXEC_OTHER, 0, "out of memory");
    return NULL;
  }
  pid_t pid = fork();
  if (pid < 0) {
    zyxexec_failed();
    zyxexec_free_argv(argv);
    for (int i = 0; i < 2; i++) {
      if (in_pipe[i] >= 0) close(in_pipe[i]);
      if (out_pipe[i] >= 0) close(out_pipe[i]);
      if (err_pipe[i] >= 0) close(err_pipe[i]);
    }
    return NULL;
  }
  if (pid == 0) {
    /* The child. Its streams are set before the program replaces it, and a
       program that cannot be replaced ends at once. */
    if (mode == ZYXEXEC_PIPE) {
      dup2(in_pipe[0], 0);
      dup2(out_pipe[1], 1);
      dup2(err_pipe[1], 2);
      close(in_pipe[0]);
      close(in_pipe[1]);
      close(out_pipe[0]);
      close(out_pipe[1]);
      close(err_pipe[0]);
      close(err_pipe[1]);
    }
    execvp(cmd, argv);
    _exit(127);
  }
  zyxexec_free_argv(argv);
  zyxexec_child *child = (zyxexec_child *)calloc(1, sizeof(zyxexec_child));
  if (child == NULL) {
    zyxexec_set(ZYXEXEC_OTHER, 0, "out of memory");
    kill(pid, SIGKILL);
    waitpid(pid, NULL, 0);
    for (int i = 0; i < 2; i++) {
      if (in_pipe[i] >= 0) close(in_pipe[i]);
      if (out_pipe[i] >= 0) close(out_pipe[i]);
      if (err_pipe[i] >= 0) close(err_pipe[i]);
    }
    return NULL;
  }
  child->pid = (int32_t)pid;
  if (mode == ZYXEXEC_PIPE) {
    /* The child holds the other ends now, so this process lets go of them. */
    close(in_pipe[0]);
    close(out_pipe[1]);
    close(err_pipe[1]);
    child->in = zyxexec_file_new(in_pipe[1]);
    child->out = zyxexec_file_new(out_pipe[0]);
    child->err = zyxexec_file_new(err_pipe[0]);
  }
  return child;
}

int32_t zyxexec_pid(void *handle) {
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL) return 0;
  return child->pid;
}

void *zyxexec_stream(void *handle, int32_t which) {
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL) return NULL;
  switch (which) {
    case 0:
      return child->in;
    case 1:
      return child->out;
    case 2:
      return child->err;
    default:
      return NULL;
  }
}

/* zyxexec_outcome reads a status the wait call filled. */
static void zyxexec_outcome(int status, int32_t *code, int32_t *signal) {
  if (WIFSIGNALED(status)) {
    *code = 0;
    *signal = (int32_t)WTERMSIG(status);
  } else {
    *code = (int32_t)WEXITSTATUS(status);
    *signal = 0;
  }
}

int32_t zyxexec_wait(void *handle, int32_t *code, int32_t *signal) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->pid == 0) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  int status = 0;
  pid_t got = waitpid(child->pid, &status, 0);
  if (got < 0) {
    zyxexec_failed();
    return -1;
  }
  child->pid = 0;
  zyxexec_outcome(status, code, signal);
  return 0;
}

int32_t zyxexec_poll(void *handle, int32_t *code, int32_t *signal) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->pid == 0) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  int status = 0;
  pid_t got = waitpid(child->pid, &status, WNOHANG);
  if (got < 0) {
    zyxexec_failed();
    return -1;
  }
  if (got == 0) return 0;
  child->pid = 0;
  zyxexec_outcome(status, code, signal);
  return 1;
}

int32_t zyxexec_kill(void *handle) {
  zyxexec_clear();
  zyxexec_child *child = (zyxexec_child *)handle;
  if (child == NULL || child->pid == 0) {
    zyxexec_set(ZYXEXEC_CLOSED, 0, "the child was already reaped");
    return -1;
  }
  if (kill(child->pid, SIGKILL) != 0) {
    zyxexec_failed();
    return -1;
  }
  return 0;
}

void zyxexec_free(void *handle) {
  free(handle);
}

#endif
