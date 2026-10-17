/* The database calls of std/sqlite: the connection a program opens, the
   statements it runs through it, and the failure the last call reported. */

#ifndef ZYXSQLITE_H
#define ZYXSQLITE_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. */
#define ZYXSQLITE_NOT_FOUND 1
#define ZYXSQLITE_PERMISSION 2
#define ZYXSQLITE_EXISTS 3
#define ZYXSQLITE_NOT_DIR 4
#define ZYXSQLITE_IS_DIR 5
#define ZYXSQLITE_INVALID 6
#define ZYXSQLITE_CLOSED 7
#define ZYXSQLITE_TIMEOUT 8
#define ZYXSQLITE_INTERRUPTED 9
#define ZYXSQLITE_WOULD_BLOCK 10
#define ZYXSQLITE_EOF 11
#define ZYXSQLITE_OTHER 12

int32_t zyxsqlite_error_kind(void);
int32_t zyxsqlite_error_number(void);
void zyxsqlite_error_message(void *buf);

/* zyxsqlite_open opens the database a path names, making the file if it is
   not there. An answer of no address is read through the failure the call
   left behind. */
void *zyxsqlite_open(const char *path, size_t len);
/* zyxsqlite_close gives a connection up. A statement that is still open is
   released with it. */
int32_t zyxsqlite_close(void *db);

/* zyxsqlite_prepare compiles the one statement a text holds and answers it.
   An answer of no address means the text held no statement, or more than
   one, which the failure says. */
void *zyxsqlite_prepare(void *db, const char *sql, size_t len);
/* zyxsqlite_finalize releases a statement. The error a statement that was not
   run to its end would report is the one its last step already reported, so
   the answer is not read here. */
void zyxsqlite_finalize(void *stmt);
/* zyxsqlite_step runs a statement to its next row: one when a row is ready,
   zero when there is none left, and a negative number on a failure. */
int32_t zyxsqlite_step(void *stmt);

/* zyxsqlite_bind_* set the value of parameter index, which SQLite numbers
   from one in the order the statement writes its placeholders. Each answers
   zero, or a fault. */
int32_t zyxsqlite_bind_null(void *stmt, int32_t index);
int32_t zyxsqlite_bind_int(void *stmt, int32_t index, int64_t value);
int32_t zyxsqlite_bind_float(void *stmt, int32_t index, double value);
int32_t zyxsqlite_bind_text(void *stmt, int32_t index, const char *text, size_t len);

/* zyxsqlite_changes answers how many rows the last statement of a connection
   changed. */
int64_t zyxsqlite_changes(void *db);

/* The result columns of a statement. A name and a text are appended to a
   buffer, and the bytes of a text are the ones SQLite holds, without the zero
   byte that ends a C string. A column that holds no value writes nothing. */
int32_t zyxsqlite_column_count(void *stmt);
void zyxsqlite_column_name(void *stmt, int32_t index, void *buf);
int32_t zyxsqlite_column_null(void *stmt, int32_t index);
int64_t zyxsqlite_column_int(void *stmt, int32_t index);
double zyxsqlite_column_float(void *stmt, int32_t index);
void zyxsqlite_column_text(void *stmt, int32_t index, void *buf);

#endif /* ZYXSQLITE_H */
