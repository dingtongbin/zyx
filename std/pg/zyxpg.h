/* The PostgreSQL calls of std/pg: the connection a program opens, the
   statements it runs through it with and without parameters, the rows those
   statements answer, and the failure the last call reported. */

#ifndef ZYXPG_H
#define ZYXPG_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. */
#define ZYXPG_NOT_FOUND 1
#define ZYXPG_PERMISSION 2
#define ZYXPG_EXISTS 3
#define ZYXPG_NOT_DIR 4
#define ZYXPG_IS_DIR 5
#define ZYXPG_INVALID 6
#define ZYXPG_CLOSED 7
#define ZYXPG_TIMEOUT 8
#define ZYXPG_INTERRUPTED 9
#define ZYXPG_WOULD_BLOCK 10
#define ZYXPG_EOF 11
#define ZYXPG_ADDR_IN_USE 12
#define ZYXPG_ADDR_NOT_AVAILABLE 13
#define ZYXPG_REFUSED 14
#define ZYXPG_RESET 15
#define ZYXPG_UNREACHABLE 16
#define ZYXPG_OTHER 17

int32_t zyxpg_error_kind(void);
int32_t zyxpg_error_number(void);
void zyxpg_error_message(void *buf);

/* zyxpg_connect opens the connection a data source name names. An answer of
   no address is read through the failure the call left behind. */
void *zyxpg_connect(const char *dsn, size_t len);
/* zyxpg_close gives a connection up. */
void zyxpg_close(void *conn);

/* The parameters of a statement. A call adds one, in the order the statement
   writes its placeholders, and the whole set is given to a run. */
void *zyxpg_params_new(void);
void zyxpg_params_free(void *params);
int32_t zyxpg_params_null(void *params);
int32_t zyxpg_params_bool(void *params, int32_t value);
int32_t zyxpg_params_float(void *params, double value);

/* zyxpg_params_int adds an integer parameter, written the way the server reads
   one: an exact decimal text, which is what a wide integer needs because a float
   cannot hold every one of them. */
int32_t zyxpg_params_int(void *params, int64_t value);
int32_t zyxpg_params_text(void *params, const char *text, size_t len);

/* zyxpg_query runs a text and answers the rows it reads. An answer of no
   address is read through the failure the call left behind. */
void *zyxpg_query(void *conn, const char *sql, size_t len, void *params);
/* zyxpg_exec runs a text and answers how many rows it changed, or a negative
   number on a failure. */
int64_t zyxpg_exec(void *conn, const char *sql, size_t len, void *params);

/* The rows of a statement. zyxpg_rows_next answers one when a row is ready
   and zero when there are none left. A name and a text are appended to a
   buffer, and a text of the language is the bytes without the zero byte that
   would end a C string. */
int32_t zyxpg_rows_next(void *rows);
int32_t zyxpg_rows_columns(void *rows);
void zyxpg_rows_column_name(void *rows, int32_t index, void *buf);
int32_t zyxpg_rows_null(void *rows, int32_t index);
int64_t zyxpg_rows_int(void *rows, int32_t index);
double zyxpg_rows_float(void *rows, int32_t index);
void zyxpg_rows_text(void *rows, int32_t index, void *buf);
void zyxpg_rows_close(void *rows);

#endif /* ZYXPG_H */
