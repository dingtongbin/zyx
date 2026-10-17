/* The process calls of std/os: what a program was started with, what it
   runs in, and what it ends with. */

#ifndef ZYXOS_H
#define ZYXOS_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. */
#define ZYXOS_NOT_FOUND 1
#define ZYXOS_PERMISSION 2
#define ZYXOS_EXISTS 3
#define ZYXOS_NOT_DIR 4
#define ZYXOS_IS_DIR 5
#define ZYXOS_INVALID 6
#define ZYXOS_CLOSED 7
#define ZYXOS_TIMEOUT 8
#define ZYXOS_INTERRUPTED 9
#define ZYXOS_WOULD_BLOCK 10
#define ZYXOS_EOF 11
#define ZYXOS_OTHER 12

int32_t zyxos_error_kind(void);
int32_t zyxos_error_number(void);
void zyxos_error_message(void *buf);

/* zyxos_args appends the arguments of the process to a buffer, each followed
   by a zero byte, the name the program was started under first. */
int32_t zyxos_args(void *buf);
/* zyxos_env appends the value of a variable to a buffer and answers zero, or
   answers a fault when the variable is not there. */
int32_t zyxos_env(void *buf, const char *name, size_t len);
/* zyxos_env_all appends every variable to a buffer: the name and then the
   value of each, each followed by a zero byte. */
int32_t zyxos_env_all(void *buf);
int32_t zyxos_setenv(const char *name, size_t name_len, const char *value, size_t value_len);
int32_t zyxos_unsetenv(const char *name, size_t len);

int32_t zyxos_getpid(void);
/* zyxos_standard answers the handle of one of the three streams the process
   begins with, which is the file package's own handle: this package imports
   that one, so the two meet at the link. */
void *zyxos_standard(int32_t which);
/* zyxos_os and zyxos_arch append the segment of the target the program was
   built for: the one the platform matrix names. */
void zyxos_os(void *buf);
void zyxos_arch(void *buf);
int32_t zyxos_hostname(void *buf);
/* zyxos_exit ends the process at once, with the code it is given. */
void zyxos_exit(int32_t code);

#endif /* ZYXOS_H */
