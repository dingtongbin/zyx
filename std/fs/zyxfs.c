#include "zyxfs.h"

#include "zyxrt.h"

/* The C compiler is asked for strict ISO C, under which a system hides the
   declarations this file is written against, and the file offsets of a 32-bit
   system are too narrow for the files of a modern one. Both words have to
   come before any system header. */
#if !defined(_WIN32)
#if !defined(_GNU_SOURCE)
#define _GNU_SOURCE 1
#endif
#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif
#endif

#if !defined(_FILE_OFFSET_BITS)
#define _FILE_OFFSET_BITS 64
#endif

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

#if defined(_WIN32)

#include <direct.h>
#include <io.h>

/* A text-mode read of a Windows file would rewrite its line endings, and this
   package reads and writes the bytes a file holds. */
#define ZYXFS_BINARY _O_BINARY

#else

#include <unistd.h>

#define ZYXFS_BINARY 0

#endif

#include <dirent.h>

/* The failure state of the calling thread. A call reports a failure by
   leaving what went wrong here, which the caller reads at once. */
static _Thread_local int32_t zyxfs_kind;
static _Thread_local int32_t zyxfs_errno;
static _Thread_local char zyxfs_text[256];

static void zyxfs_fail(int32_t kind, int number) {
  zyxfs_kind = kind;
  zyxfs_errno = number;
  const char *text = strerror(number);
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  if (n >= sizeof(zyxfs_text)) n = sizeof(zyxfs_text) - 1;
  memcpy(zyxfs_text, text, n);
  zyxfs_text[n] = 0;
}

/* zyxfs_clear forgets the failure of the call before, so a call that
   succeeded cannot be read as a failure. */
static void zyxfs_clear(void) {
  zyxfs_kind = 0;
  zyxfs_errno = 0;
  zyxfs_text[0] = 0;
}

int32_t zyxfs_error_kind(void) { return zyxfs_kind; }

int32_t zyxfs_error_number(void) { return zyxfs_errno; }

void zyxfs_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxfs_text, strlen(zyxfs_text));
}

/* kind_of maps the platform's own error number onto the shape every system
   package reports. The numbers are the platform's, so the mapping is written
   where the numbers are. */
static int32_t kind_of(int number) {
  switch (number) {
    case ENOENT:
      return ZYXFS_NOT_FOUND;
    case EACCES:
    case EPERM:
      return ZYXFS_PERMISSION;
    case EEXIST:
      return ZYXFS_EXISTS;
    case ENOTDIR:
      return ZYXFS_NOT_DIR;
    case EISDIR:
      return ZYXFS_IS_DIR;
    case EINVAL:
      return ZYXFS_INVALID;
    case EBADF:
      return ZYXFS_CLOSED;
    case EINTR:
      return ZYXFS_INTERRUPTED;
#if defined(EWOULDBLOCK) && EWOULDBLOCK != EAGAIN
    case EWOULDBLOCK:
#endif
    case EAGAIN:
      return ZYXFS_WOULD_BLOCK;
    default:
      return ZYXFS_OTHER;
  }
}

/* zyxfs_failed records the failure the platform just reported and answers the
   count a caller reads as a fault. */
static int64_t zyxfs_failed(void) {
  zyxfs_fail(kind_of(errno), errno);
  return -1;
}

static void zyxfs_failed_int(void) { zyxfs_fail(kind_of(errno), errno); }

/* cpath copies the bytes of a path into a terminated string, because the
   platform's calls want one and a str of the language is a view without a
   terminator. */
static char *cpath(const char *path, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, path, len);
  out[len] = 0;
  return out;
}

/* A file is a descriptor and whether this package owns it. The three the
   process begins with are wrapped in static objects, because they outlive
   every call. */
typedef struct {
  int fd;
  int owned;
} zyxfs_file;

static zyxfs_file zyxfs_standard_files[3] = {{0, 0}, {1, 0}, {2, 0}};

void *zyxfs_standard(int32_t which) {
  if (which < 0 || which > 2) return NULL;
  return &zyxfs_standard_files[which];
}

void *zyxfs_open(const char *path, size_t len, int32_t mode) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return NULL;
  }
  int flags = 0;
  switch (mode) {
    case ZYXFS_CREATE:
      flags = O_WRONLY | O_CREAT | O_TRUNC;
      break;
    case ZYXFS_APPEND:
      flags = O_WRONLY | O_CREAT | O_APPEND;
      break;
    default:
      flags = O_RDONLY;
      break;
  }
  int fd = open(name, flags | ZYXFS_BINARY, 0666);
  if (fd < 0) {
    /* A directory that a read-only open refused on one platform and the
       platform's own call refused on another reports the same kind. */
    struct stat st;
    if (stat(name, &st) == 0 && S_ISDIR(st.st_mode)) {
      zyxfs_fail(ZYXFS_IS_DIR, errno);
    } else {
      zyxfs_failed_int();
    }
    free(name);
    return NULL;
  }
  free(name);
  zyxfs_file *file = (zyxfs_file *)malloc(sizeof(zyxfs_file));
  if (file == NULL) {
    close(fd);
    zyxfs_fail(ZYXFS_OTHER, 0);
    return NULL;
  }
  file->fd = fd;
  file->owned = 1;
  return file;
}

void zyxfs_close(void *handle) {
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) return;
  if (file->owned) {
    close(file->fd);
    free(file);
  }
}

int64_t zyxfs_read(void *handle, void *buf, size_t cap) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  if (cap == 0) return 0;
  /* The read lands in a scratch buffer and is handed to the language's own
     buffer, because a call of this package may not write through a view the
     language owns. A read is asked for at most one chunk, so the caller
     loops for the rest. */
  if (cap > 65536) cap = 65536;
  unsigned char *scratch = (unsigned char *)malloc(cap);
  if (scratch == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  int64_t n = (int64_t)read(file->fd, scratch, (unsigned int)cap);
  if (n > 0) zyxrt_buf_write(buf, scratch, (size_t)n);
  free(scratch);
  if (n < 0) {
    /* A read that was interrupted before it moved anything is not a fault:
       the caller asks again. */
    if (errno == EINTR) return 0;
    return zyxfs_failed();
  }
  return n;
}

/* The two streams a program prints to carry the C runtime's own buffer,
   which is what the runtime writes its text through. A write through a raw
   descriptor would pass the bytes the buffer still holds, so a write to one
   of the two goes through the same stream. */
static FILE *zyxfs_stream(zyxfs_file *file) {
  if (file->fd == 1) return stdout;
  if (file->fd == 2) return stderr;
  return NULL;
}

int64_t zyxfs_write(void *handle, const unsigned char *buf, size_t len) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  if (len == 0) return 0;
  FILE *stream = zyxfs_stream(file);
  if (stream != NULL) {
    size_t n = fwrite(buf, 1, len, stream);
    return (int64_t)n;
  }
  int64_t n = (int64_t)write(file->fd, buf, (unsigned int)len);
  if (n < 0) return zyxfs_failed();
  return n;
}

int64_t zyxfs_write_all(void *handle, const unsigned char *buf, size_t len) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  FILE *stream = zyxfs_stream(file);
  if (stream != NULL) {
    size_t n = fwrite(buf, 1, len, stream);
    if (n != len) {
      zyxfs_fail(ZYXFS_OTHER, 0);
      return -1;
    }
    return (int64_t)n;
  }
  size_t done = 0;
  while (done < len) {
    int64_t n = (int64_t)write(file->fd, buf + done, (unsigned int)(len - done));
    if (n < 0) {
      if (errno == EINTR) continue;
      return zyxfs_failed();
    }
    if (n == 0) {
      zyxfs_fail(ZYXFS_EOF, 0);
      return -1;
    }
    done += (size_t)n;
  }
  return (int64_t)done;
}

int64_t zyxfs_seek(void *handle, int64_t off, int32_t whence) {  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  off_t at = lseek(file->fd, (off_t)off, whence);
  if (at == (off_t)-1) return zyxfs_failed();
  return (int64_t)at;
}

int32_t zyxfs_sync(void *handle) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  /* The call that waits for a file to reach the disk carries a different
     name on each of the two families of targets. */
#if defined(_WIN32)
  if (_commit(file->fd) != 0) {
#else
  if (fsync(file->fd) != 0) {
#endif
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

/* fill_stat writes what one stat answered into the shape the language reads. */
static void fill_stat(const struct stat *st, zyxfs_info *out) {
  out->size = (int64_t)st->st_size;
  out->mode = (uint32_t)(st->st_mode & 07777);
  out->is_dir = S_ISDIR(st->st_mode) ? 1 : 0;
  out->mtime_nanos = (int64_t)st->st_mtime * 1000000000ll;
}

int32_t zyxfs_stat_file(void *handle, zyxfs_info *out) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return -1;
  }
  struct stat st;
  if (fstat(file->fd, &st) != 0) {
    zyxfs_failed_int();
    return -1;
  }
  fill_stat(&st, out);
  return 0;
}

int32_t zyxfs_stat_path(const char *path, size_t len, zyxfs_info *out) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  struct stat st;
  if (stat(name, &st) != 0) {
    free(name);
    zyxfs_failed_int();
    return -1;
  }
  free(name);
  fill_stat(&st, out);
  return 0;
}

int32_t zyxfs_remove(const char *path, size_t len) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  int rc = remove(name);
  free(name);
  if (rc != 0) {
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

int32_t zyxfs_rename(const char *from, size_t from_len, const char *to, size_t to_len) {
  zyxfs_clear();
  char *old_name = cpath(from, from_len);
  char *new_name = cpath(to, to_len);
  if (old_name == NULL || new_name == NULL) {
    free(old_name);
    free(new_name);
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  int rc = rename(old_name, new_name);
  free(old_name);
  free(new_name);
  if (rc != 0) {
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

int32_t zyxfs_mkdir(const char *path, size_t len, uint32_t mode) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  /* The C runtime of Windows makes a directory with a call that takes no
     mode, because the system has no notion of one for it. */
#if defined(_WIN32)
  int rc = mkdir(name);
#else
  int rc = mkdir(name, (mode_t)mode);
#endif
  if (rc != 0) {
    /* A directory that is already there is what the caller meant to make, and
       the platforms disagree about the number they report for it. */
    struct stat st;
    if (stat(name, &st) == 0 && S_ISDIR(st.st_mode)) {
      zyxfs_fail(ZYXFS_EXISTS, errno);
    } else {
      zyxfs_failed_int();
    }
    free(name);
    return -1;
  }
  free(name);
  return 0;
}

int32_t zyxfs_rmdir(const char *path, size_t len) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  int rc = rmdir(name);
  free(name);
  if (rc != 0) {
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

int32_t zyxfs_chmod(const char *path, size_t len, uint32_t mode) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  int rc = chmod(name, (mode_t)mode);
  free(name);
  if (rc != 0) {
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

/* The names a directory read collected, and the comparison that puts them in
   byte order. */
typedef struct {
  char **names;
  size_t count;
  size_t cap;
} zyxfs_list;

static int zyxfs_compare(const void *a, const void *b) {
  const char *left = *(const char *const *)a;
  const char *right = *(const char *const *)b;
  return strcmp(left, right);
}

int32_t zyxfs_read_dir(void *buf, const char *path, size_t len) {
  zyxfs_clear();
  char *name = cpath(path, len);
  if (name == NULL) {
    zyxfs_fail(ZYXFS_OTHER, 0);
    return -1;
  }
  DIR *dir = opendir(name);
  free(name);
  if (dir == NULL) {
    zyxfs_failed_int();
    return -1;
  }
  zyxfs_list list = {NULL, 0, 0};
  int32_t fault = 0;
  for (;;) {
    errno = 0;
    struct dirent *entry = readdir(dir);
    if (entry == NULL) {
      if (errno != 0) {
        zyxfs_failed_int();
        fault = -1;
      }
      break;
    }
    const char *entry_name = entry->d_name;
    if (strcmp(entry_name, ".") == 0 || strcmp(entry_name, "..") == 0) continue;
    if (list.count == list.cap) {
      size_t cap = list.cap == 0 ? 16 : list.cap * 2;
      char **grown = (char **)realloc(list.names, cap * sizeof(char *));
      if (grown == NULL) {
        zyxfs_fail(ZYXFS_OTHER, 0);
        fault = -1;
        break;
      }
      list.names = grown;
      list.cap = cap;
    }
    size_t n = strlen(entry_name);
    char *copy = (char *)malloc(n + 1);
    if (copy == NULL) {
      zyxfs_fail(ZYXFS_OTHER, 0);
      fault = -1;
      break;
    }
    memcpy(copy, entry_name, n + 1);
    list.names[list.count++] = copy;
  }
  closedir(dir);
  if (fault == 0) {
    qsort(list.names, list.count, sizeof(char *), zyxfs_compare);
    for (size_t i = 0; i < list.count; i++) {
      size_t n = strlen(list.names[i]);
      zyxrt_buf_write(buf, (const unsigned char *)list.names[i], n);
      unsigned char zero = 0;
      zyxrt_buf_write(buf, &zero, 1);
    }
  }
  for (size_t i = 0; i < list.count; i++) free(list.names[i]);
  free(list.names);
  return fault;
}

int32_t zyxfs_cwd(void *buf) {
  zyxfs_clear();
  char text[4096];
  if (getcwd(text, sizeof(text)) == NULL) {
    zyxfs_failed_int();
    return -1;
  }
  zyxrt_buf_write(buf, (const unsigned char *)text, strlen(text));
  return 0;
}

/* zyxfs_temp_dir appends the directory a temporary file belongs in. The
   variables the system defines come first, and a directory of the process
   itself is the answer when it names none. */
int32_t zyxfs_temp_dir(void *buf) {
  zyxfs_clear();
  const char *name = getenv("TMPDIR");
#if defined(_WIN32)
  if (name == NULL || name[0] == 0) name = getenv("TEMP");
#endif
  if (name == NULL || name[0] == 0) name = getenv("TMP");
#if !defined(_WIN32)
  if (name == NULL || name[0] == 0) name = "/tmp";
#endif
  if (name == NULL || name[0] == 0) name = ".";
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
  return 0;
}

/* zyxfs_temp_file creates a file whose name no other call has used: a counter
   the process owns and the process's own number, tried until the platform
   says the name is new. */
static unsigned long zyxfs_counter;

void *zyxfs_temp_file(const char *dir, size_t dir_len, const char *prefix, size_t prefix_len) {
  zyxfs_clear();
  char *base = cpath(dir, dir_len);
  char *head = cpath(prefix, prefix_len);
  if (base == NULL || head == NULL) {
    free(base);
    free(head);
    zyxfs_fail(ZYXFS_OTHER, 0);
    return NULL;
  }
  size_t base_len = strlen(base);
  size_t head_len = strlen(head);
  char *name = (char *)malloc(base_len + head_len + 64);
  if (name == NULL) {
    free(base);
    free(head);
    zyxfs_fail(ZYXFS_OTHER, 0);
    return NULL;
  }
  for (int attempt = 0; attempt < 256; attempt++) {
    unsigned long n = ++zyxfs_counter;
    int written = snprintf(name, base_len + head_len + 64, "%s/%s%lu", base, head, n);
    if (written <= 0) break;
    int fd = open(name, O_RDWR | O_CREAT | O_EXCL | ZYXFS_BINARY, 0600);
    if (fd >= 0) {
      zyxfs_file *file = (zyxfs_file *)malloc(sizeof(zyxfs_file));
      if (file == NULL) {
        close(fd);
        break;
      }
      file->fd = fd;
      file->owned = 1;
      free(base);
      free(head);
      free(name);
      return file;
    }
    if (errno != EEXIST) break;
  }
  zyxfs_fail(kind_of(errno), errno);
  free(base);
  free(head);
  free(name);
  return NULL;
}

/* Memory mapping. A mapping of a file is made with the call each platform
   has, and what a platform needs to undo it is kept where unmapping can find
   it. */
#if defined(_WIN32)

#include <windows.h>

typedef struct {
  void *view;
  HANDLE section;
} zyxfs_mapping;

static zyxfs_mapping *zyxfs_mappings;
static size_t zyxfs_mapping_count;

void *zyxfs_mmap(void *handle, size_t len, uint64_t off) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return NULL;
  }
  intptr_t os_handle = _get_osfhandle(file->fd);
  if (os_handle == -1) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return NULL;
  }
  HANDLE section = CreateFileMappingA((HANDLE)os_handle, NULL, PAGE_READWRITE, 0, 0, NULL);
  DWORD access = FILE_MAP_ALL_ACCESS;
  if (section == NULL) {
    /* A file the caller opened for reading only is mapped for reading, which
       is what the system allows of it. */
    section = CreateFileMappingA((HANDLE)os_handle, NULL, PAGE_READONLY, 0, 0, NULL);
    access = FILE_MAP_READ;
  }
  if (section == NULL) {
    zyxfs_fail(kind_of(errno), errno);
    return NULL;
  }
  void *view = MapViewOfFile(section, access, (DWORD)(off >> 32), (DWORD)(off & 0xffffffffu), len);
  if (view == NULL) {
    CloseHandle(section);
    zyxfs_fail(kind_of(errno), errno);
    return NULL;
  }
  zyxfs_mapping *grown = (zyxfs_mapping *)realloc(zyxfs_mappings, (zyxfs_mapping_count + 1) * sizeof(zyxfs_mapping));
  if (grown == NULL) {
    UnmapViewOfFile(view);
    CloseHandle(section);
    zyxfs_fail(ZYXFS_OTHER, 0);
    return NULL;
  }
  zyxfs_mappings = grown;
  zyxfs_mappings[zyxfs_mapping_count].view = view;
  zyxfs_mappings[zyxfs_mapping_count].section = section;
  zyxfs_mapping_count++;
  return view;
}

int32_t zyxfs_munmap(void *p, size_t len) {
  zyxfs_clear();
  (void)len;
  for (size_t i = 0; i < zyxfs_mapping_count; i++) {
    if (zyxfs_mappings[i].view != p) continue;
    UnmapViewOfFile(p);
    CloseHandle(zyxfs_mappings[i].section);
    zyxfs_mappings[i] = zyxfs_mappings[zyxfs_mapping_count - 1];
    zyxfs_mapping_count--;
    return 0;
  }
  zyxfs_fail(ZYXFS_INVALID, 0);
  return -1;
}

#else

#include <sys/mman.h>

void *zyxfs_mmap(void *handle, size_t len, uint64_t off) {
  zyxfs_clear();
  zyxfs_file *file = (zyxfs_file *)handle;
  if (file == NULL) {
    zyxfs_fail(ZYXFS_CLOSED, 0);
    return NULL;
  }
  void *p = mmap(NULL, len, PROT_READ | PROT_WRITE, MAP_SHARED, file->fd, (off_t)off);
  if (p == MAP_FAILED && errno == EACCES) {
    /* A file the caller opened for reading only is mapped for reading, which
       is what the system allows of it. */
    p = mmap(NULL, len, PROT_READ, MAP_SHARED, file->fd, (off_t)off);
  }
  if (p == MAP_FAILED) {
    zyxfs_failed();
    return NULL;
  }
  return p;
}

int32_t zyxfs_munmap(void *p, size_t len) {
  zyxfs_clear();
  if (munmap(p, len) != 0) {
    zyxfs_failed_int();
    return -1;
  }
  return 0;
}

#endif
