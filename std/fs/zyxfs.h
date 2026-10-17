/* The file system calls of std/fs. The package's own C reaches the operating
   system through the C runtime's file interface, which every target the
   runtime serves provides, and the two places where the platforms really do
   differ (the temp directory and the memory mapping) are written twice. */

#ifndef ZYXFS_H
#define ZYXFS_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. Every call reports a failure by leaving
   one of these, the platform's own error number, and the text of the
   platform's message behind, which the caller reads with the three queries
   below. A thread keeps its own, so a failure belongs to the call that made
   it. */
#define ZYXFS_NOT_FOUND 1
#define ZYXFS_PERMISSION 2
#define ZYXFS_EXISTS 3
#define ZYXFS_NOT_DIR 4
#define ZYXFS_IS_DIR 5
#define ZYXFS_INVALID 6
#define ZYXFS_CLOSED 7
#define ZYXFS_TIMEOUT 8
#define ZYXFS_INTERRUPTED 9
#define ZYXFS_WOULD_BLOCK 10
#define ZYXFS_EOF 11
#define ZYXFS_OTHER 12

int32_t zyxfs_error_kind(void);
int32_t zyxfs_error_number(void);
/* zyxfs_error_message appends the text of the last failure to a buffer. */
void zyxfs_error_message(void *buf);

/* The modes of zyxfs_open: read, create or truncate and write, and append. */
#define ZYXFS_READ 1
#define ZYXFS_CREATE 2
#define ZYXFS_APPEND 3

/* What a stat answers. It is a C layout, so the language's own declaration of
   the same fields reads what the caller wrote here. */
typedef struct {
  int64_t size;
  uint32_t mode;
  int32_t is_dir;
  int64_t mtime_nanos;
} zyxfs_info;

/* A file handle, which is one of the standard streams for the three the
   process begins with. */
void *zyxfs_open(const char *path, size_t len, int32_t mode);
void *zyxfs_standard(int32_t which);
void zyxfs_close(void *file);
/* zyxfs_read moves at most cap bytes into the language's own byte buffer,
   which the caller hands over as its handle. */
int64_t zyxfs_read(void *file, void *buf, size_t cap);
int64_t zyxfs_write(void *file, const unsigned char *buf, size_t len);
/* zyxfs_write_all writes every byte of a buffer, or reports the failure that
   stopped it. */
int64_t zyxfs_write_all(void *file, const unsigned char *buf, size_t len);
int64_t zyxfs_seek(void *file, int64_t off, int32_t whence);
int32_t zyxfs_sync(void *file);
int32_t zyxfs_stat_file(void *file, zyxfs_info *out);

int32_t zyxfs_stat_path(const char *path, size_t len, zyxfs_info *out);
int32_t zyxfs_remove(const char *path, size_t len);
int32_t zyxfs_rename(const char *from, size_t from_len, const char *to, size_t to_len);
int32_t zyxfs_mkdir(const char *path, size_t len, uint32_t mode);
int32_t zyxfs_rmdir(const char *path, size_t len);
int32_t zyxfs_chmod(const char *path, size_t len, uint32_t mode);
/* zyxfs_read_dir appends the name of every entry of a directory to a buffer,
   in byte order, each followed by a zero byte, with the two entries that name
   the directory itself left out. */
int32_t zyxfs_read_dir(void *buf, const char *path, size_t len);
/* zyxfs_cwd appends the directory the process runs in to a buffer. */
int32_t zyxfs_cwd(void *buf);
/* zyxfs_temp_dir appends the directory a temporary file belongs in. */
int32_t zyxfs_temp_dir(void *buf);
/* zyxfs_temp_file creates a file whose name no other call has used. */
void *zyxfs_temp_file(const char *dir, size_t dir_len, const char *prefix, size_t prefix_len);

/* Memory mapping. A mapping of a file is written by the same call on both
   platforms; unmapping asks the table a mapping was entered in. */
void *zyxfs_mmap(void *file, size_t len, uint64_t off);
int32_t zyxfs_munmap(void *p, size_t len);

#endif /* ZYXFS_H */
