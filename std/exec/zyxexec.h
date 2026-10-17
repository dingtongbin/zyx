/* The process calls of std/exec: starting a child, waiting for it, killing
   it, and reading and writing the pipes a child's standard streams are. */

#ifndef ZYXEXEC_H
#define ZYXEXEC_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. Every call reports a failure by leaving
   one of these, the platform's own error number, and the text of the
   platform's message behind, which the caller reads with the three queries
   below. A thread keeps its own, so a failure belongs to the call that made
   it. */
#define ZYXEXEC_NOT_FOUND 1
#define ZYXEXEC_PERMISSION 2
#define ZYXEXEC_EXISTS 3
#define ZYXEXEC_NOT_DIR 4
#define ZYXEXEC_IS_DIR 5
#define ZYXEXEC_INVALID 6
#define ZYXEXEC_CLOSED 7
#define ZYXEXEC_TIMEOUT 8
#define ZYXEXEC_INTERRUPTED 9
#define ZYXEXEC_WOULD_BLOCK 10
#define ZYXEXEC_EOF 11
#define ZYXEXEC_OTHER 12

int32_t zyxexec_error_kind(void);
int32_t zyxexec_error_number(void);
/* zyxexec_error_message appends the text of the last failure to a buffer. */
void zyxexec_error_message(void *buf);

/* The two ways a child is given its standard streams. */
#define ZYXEXEC_INHERIT 0
#define ZYXEXEC_PIPE 1

/* zyxexec_start starts a child. cmd names the program, args holds its
   arguments without the program name, each argument ended by a zero byte, and
   mode is one of the two above. It answers the child record, or nothing and a
   failure. */
void *zyxexec_start(const char *cmd, size_t cmd_len, const char *args, size_t args_len, int32_t mode);

/* zyxexec_pid answers the number the system gave the child. */
int32_t zyxexec_pid(void *child);

/* zyxexec_stream answers one of the pipes of a child started in pipe mode:
   which is 0 for its input, 1 for its output, and 2 for its error. The handle
   is the one the file package reads: that package owns the descriptor, so
   closing the handle with it closes the descriptor and frees the handle too.
   It answers nothing for a child whose streams were inherited. */
void *zyxexec_stream(void *child, int32_t which);

/* zyxexec_wait waits for the child to end and reaps it, and answers zero with
   the exit code, or the number of the signal that ended it in place of the
   code. A wait that itself failed answers a fault. */
int32_t zyxexec_wait(void *child, int32_t *code, int32_t *signal);

/* zyxexec_poll answers 1 with the outcome when the child has ended, zero when
   it is still running, and a fault when the check itself failed. */
int32_t zyxexec_poll(void *child, int32_t *code, int32_t *signal);

/* zyxexec_kill ends the child with the strongest means the platform has. */
int32_t zyxexec_kill(void *child);

/* zyxexec_free gives the child record back. Take care that the pipes do not
   belong to it: the handles the caller was given own them, and the file
   package frees them when they are closed. */
void zyxexec_free(void *child);

#endif /* ZYXEXEC_H */
