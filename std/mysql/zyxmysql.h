/* The MySQL calls of std/mysql: the connection a program opens, the
   statements it runs through it with and without parameters, the rows those
   statements answer, and the failure the last call reported. */

#ifndef ZYXMYSQL_H
#define ZYXMYSQL_H

#include <stddef.h>
#include <stdint.h>

/* The kind of a failure, in the one shape the system packages share, in the
   order of the language's ErrorKind. */
#define ZYXMYSQL_NOT_FOUND 1
#define ZYXMYSQL_PERMISSION 2
#define ZYXMYSQL_EXISTS 3
#define ZYXMYSQL_NOT_DIR 4
#define ZYXMYSQL_IS_DIR 5
#define ZYXMYSQL_INVALID 6
#define ZYXMYSQL_CLOSED 7
#define ZYXMYSQL_TIMEOUT 8
#define ZYXMYSQL_INTERRUPTED 9
#define ZYXMYSQL_WOULD_BLOCK 10
#define ZYXMYSQL_EOF 11
#define ZYXMYSQL_ADDR_IN_USE 12
#define ZYXMYSQL_ADDR_NOT_AVAILABLE 13
#define ZYXMYSQL_REFUSED 14
#define ZYXMYSQL_RESET 15
#define ZYXMYSQL_UNREACHABLE 16
#define ZYXMYSQL_OTHER 17

int32_t zyxmysql_error_kind(void);
int32_t zyxmysql_error_number(void);
void zyxmysql_error_message(void *buf);

/* zyxmysql_connect opens a connection. A net of zero is the stream protocol
   to host and port, and a net of one is the unix protocol to the socket path
   host names. An answer of no address is read through the failure the call
   left behind. */
void *zyxmysql_connect(int32_t net,
                       const char *host, size_t host_len, int32_t port,
                       const char *user, size_t user_len,
                       const char *password, size_t password_len,
                       const char *dbname, size_t dbname_len);
/* zyxmysql_close gives a connection up. */
void zyxmysql_close(void *conn);

/* zyxmysql_exec runs a text of one statement that takes no parameters and
   answers how many rows it changed, or a negative number on a failure. */
int64_t zyxmysql_exec(void *conn, const char *sql, size_t len);
/* zyxmysql_query runs a text of one statement that takes no parameters and
   answers rows. An answer of no address is read through the failure the call
   left behind. */
void *zyxmysql_query(void *conn, const char *sql, size_t len);

/* The prepared statements. zyxmysql_prepare answers a statement whose
   parameters are set one by one; zyxmysql_execute runs it and answers the
   rows it changed, or a negative number on a failure; zyxmysql_result answers
   the rows it reads and takes the statement over, so the statement is not
   closed by the caller and is closed when the rows are. */
void *zyxmysql_prepare(void *conn, const char *sql, size_t len);
int32_t zyxmysql_param_count(void *stmt);
int32_t zyxmysql_bind_null(void *stmt, int32_t index);
int32_t zyxmysql_bind_int(void *stmt, int32_t index, int64_t value);
int32_t zyxmysql_bind_float(void *stmt, int32_t index, double value);
int32_t zyxmysql_bind_text(void *stmt, int32_t index, const char *text, size_t len);
int64_t zyxmysql_execute(void *stmt);
void *zyxmysql_result(void *stmt);
void zyxmysql_stmt_close(void *stmt);

/* The rows of a statement. zyxmysql_rows_next answers one when a row is
   ready, zero when there are none left, and a negative number on a failure.
   A name and a text are appended to a buffer, and a text of the language is
   the bytes without the zero byte that would end a C string. */
int32_t zyxmysql_rows_next(void *rows);
int32_t zyxmysql_rows_columns(void *rows);
void zyxmysql_rows_column_name(void *rows, int32_t index, void *buf);
int32_t zyxmysql_rows_null(void *rows, int32_t index);
int64_t zyxmysql_rows_int(void *rows, int32_t index);
double zyxmysql_rows_float(void *rows, int32_t index);
void zyxmysql_rows_text(void *rows, int32_t index, void *buf);
void zyxmysql_rows_close(void *rows);

#endif /* ZYXMYSQL_H */
