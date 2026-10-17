#include "zyxmysql.h"

#include "zyxrt.h"

#include <mariadb/mysql.h>
#include <stdlib.h>
#include <string.h>

/* The failure state of the calling thread, in the shape every system package
   reports one. A call of this package runs on the thread of the coroutine
   that made it, so the state is read back by the call after it. */
static _Thread_local int32_t zyxmysql_kind;
static _Thread_local int32_t zyxmysql_code;
static _Thread_local char zyxmysql_text[512];

static void zyxmysql_clear(void) {
  zyxmysql_kind = 0;
  zyxmysql_code = 0;
  zyxmysql_text[0] = 0;
}

static void zyxmysql_fail(int32_t kind, int32_t code, const char *text) {
  zyxmysql_kind = kind;
  zyxmysql_code = code;
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  if (n >= sizeof(zyxmysql_text)) n = sizeof(zyxmysql_text) - 1;
  memcpy(zyxmysql_text, text, n);
  zyxmysql_text[n] = 0;
}

/* zyxmysql_kind_of maps a client or server error number onto the kind of
   failure it is. The client numbers are the CR_ ones the connection calls
   report, and the server numbers are the ER_ ones a statement reports. */
static int32_t zyxmysql_kind_of(unsigned int code) {
  switch (code) {
    case 1044: /* access denied for the database */
    case 1045: /* access denied for the user */
    case 1698: /* access denied: the login is not allowed */
      return ZYXMYSQL_PERMISSION;
    case 1049: /* unknown database */
    case 1051: /* unknown table */
    case 1109: /* unknown table in a multi-table statement */
    case 1146: /* table does not exist */
      return ZYXMYSQL_NOT_FOUND;
    case 1022: /* a key that must be one of a kind appears twice */
    case 1062: /* duplicate entry */
      return ZYXMYSQL_EXISTS;
    case 1054: /* unknown column */
    case 1064: /* the statement does not parse */
    case 1149: /* the statement does not parse */
    case 1264: /* a value is out of range for its column */
    case 1292: /* a value is out of range for its type */
    case 1364: /* a column that must hold a value has none */
    case 1366: /* a value is wrong for its column */
    case 1406: /* a value is too long for its column */
      return ZYXMYSQL_INVALID;
    case 1205: /* the lock wait timed out */
    case 1213: /* the transaction was rolled back by a deadlock */
      return ZYXMYSQL_WOULD_BLOCK;
    case 2002: /* the connection to the server could not be made */
    case 2003: /* the connection to the server could not be made */
      return ZYXMYSQL_REFUSED;
    case 2005: /* the host name does not answer */
      return ZYXMYSQL_UNREACHABLE;
    case 2006: /* the server is gone */
    case 2013: /* the connection to the server was lost */
    case 2055: /* the connection to the server was lost */
      return ZYXMYSQL_RESET;
    default:
      return ZYXMYSQL_OTHER;
  }
}

/* zyxmysql_fail_conn records the failure a connection reported. */
static void zyxmysql_fail_conn(MYSQL *conn, const char *fallback) {
  unsigned int code = mysql_errno(conn);
  if (code == 0) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, fallback);
    return;
  }
  zyxmysql_fail(zyxmysql_kind_of(code), (int32_t)code, mysql_error(conn));
}

/* zyxmysql_fail_stmt records the failure a statement reported. */
static void zyxmysql_fail_stmt(MYSQL_STMT *stmt, const char *fallback) {
  unsigned int code = mysql_stmt_errno(stmt);
  if (code == 0) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, fallback);
    return;
  }
  zyxmysql_fail(zyxmysql_kind_of(code), (int32_t)code, mysql_stmt_error(stmt));
}

/* zyxmysql_owned copies the bytes of a call's argument into a string that
   ends with a zero byte, because a text of the language is a view without
   one. */
static char *zyxmysql_owned(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, text, len);
  out[len] = 0;
  return out;
}

/* struct zyxmysql_value is one parameter a caller set: a kind of null, an
   integer, a floating-point number, or a text the wrapper owns. */
struct zyxmysql_value {
  int kind;
  int64_t integer;
  double floating;
  char *text;
  size_t len;
  unsigned long bound;
};

/* struct zyxmysql_stmt is a prepared statement and the parameters set on it. */
struct zyxmysql_stmt {
  MYSQL_STMT *stmt;
  unsigned long param_count;
  struct zyxmysql_value *params;
};

/* struct zyxmysql_rows is the rows of one statement, of either protocol. */
struct zyxmysql_rows {
  MYSQL *conn;
  int kind;
  MYSQL_RES *res;
  MYSQL_STMT *stmt;
  MYSQL_FIELD *fields;
  unsigned int nfields;
  /* the current row of the text protocol */
  MYSQL_ROW row;
  unsigned long *lengths;
  /* the current row of the prepared protocol */
  MYSQL_BIND *binds;
  char **data;
  size_t *cap;
  my_bool *is_null;
  my_bool *error;
  unsigned long *lens;
  int done;
  int current;
};

int32_t zyxmysql_error_kind(void) { return zyxmysql_kind; }

int32_t zyxmysql_error_number(void) { return zyxmysql_code; }

void zyxmysql_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxmysql_text, strlen(zyxmysql_text));
}

void *zyxmysql_connect(int32_t net,
                       const char *host, size_t host_len, int32_t port,
                       const char *user, size_t user_len,
                       const char *password, size_t password_len,
                       const char *dbname, size_t dbname_len) {
  zyxmysql_clear();
  MYSQL *conn = mysql_init(NULL);
  if (conn == NULL) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  unsigned int timeout = 5;
  mysql_options(conn, MYSQL_OPT_CONNECT_TIMEOUT, &timeout);
  mysql_options(conn, MYSQL_SET_CHARSET_NAME, "utf8mb4");
  char *host_text = zyxmysql_owned(host, host_len);
  char *user_text = zyxmysql_owned(user, user_len);
  char *pass_text = zyxmysql_owned(password, password_len);
  char *db_text = zyxmysql_owned(dbname, dbname_len);
  if (host_text == NULL || user_text == NULL || pass_text == NULL || db_text == NULL) {
    free(host_text);
    free(user_text);
    free(pass_text);
    free(db_text);
    mysql_close(conn);
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  /* A unix address names the socket path rather than a host, so the path
     travels in the socket argument and no host is given. */
  const char *host_arg = net == 1 ? NULL : host_text;
  const char *socket_arg = net == 1 ? host_text : NULL;
  MYSQL *opened = mysql_real_connect(conn, host_arg, user_text, pass_text, db_text,
                                     (unsigned int)port, socket_arg, 0);
  free(host_text);
  free(user_text);
  free(pass_text);
  free(db_text);
  if (opened == NULL) {
    zyxmysql_fail_conn(conn, "the connection could not be made");
    mysql_close(conn);
    return NULL;
  }
  return opened;
}

void zyxmysql_close(void *conn) {
  if (conn == NULL) return;
  mysql_close((MYSQL *)conn);
}

/* zyxmysql_read runs a text of one statement and answers its result set, or
   NULL when it answered none. A failure leaves the state behind and answers
   the second value. */
static MYSQL_RES *zyxmysql_read(MYSQL *conn, const char *sql, size_t len) {
  char *text = zyxmysql_owned(sql, len);
  if (text == NULL) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  int rc = mysql_real_query(conn, text, (unsigned long)len);
  free(text);
  if (rc != 0) {
    zyxmysql_fail_conn(conn, "the statement could not be run");
    return NULL;
  }
  MYSQL_RES *res = mysql_store_result(conn);
  if (res == NULL && mysql_field_count(conn) != 0) {
    zyxmysql_fail_conn(conn, "the result could not be read");
    return NULL;
  }
  return res;
}

void *zyxmysql_query(void *conn_handle, const char *sql, size_t len) {
  zyxmysql_clear();
  MYSQL *conn = (MYSQL *)conn_handle;
  MYSQL_RES *res = zyxmysql_read(conn, sql, len);
  if (res == NULL && zyxmysql_kind != 0) return NULL;
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)calloc(1, sizeof(*rows));
  if (rows == NULL) {
    if (res != NULL) mysql_free_result(res);
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  rows->conn = conn;
  rows->kind = 0;
  rows->res = res;
  if (res != NULL) {
    rows->nfields = mysql_num_fields(res);
    rows->fields = mysql_fetch_fields(res);
  }
  return rows;
}

int64_t zyxmysql_exec(void *conn_handle, const char *sql, size_t len) {
  zyxmysql_clear();
  MYSQL *conn = (MYSQL *)conn_handle;
  MYSQL_RES *res = zyxmysql_read(conn, sql, len);
  if (res == NULL && zyxmysql_kind != 0) return -1;
  if (res != NULL) mysql_free_result(res);
  return (int64_t)mysql_affected_rows(conn);
}

void *zyxmysql_prepare(void *conn_handle, const char *sql, size_t len) {
  zyxmysql_clear();
  MYSQL *conn = (MYSQL *)conn_handle;
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)calloc(1, sizeof(*prepared));
  if (prepared == NULL) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  prepared->stmt = mysql_stmt_init(conn);
  if (prepared->stmt == NULL) {
    free(prepared);
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  char *text = zyxmysql_owned(sql, len);
  if (text == NULL) {
    mysql_stmt_close(prepared->stmt);
    free(prepared);
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  int rc = mysql_stmt_prepare(prepared->stmt, text, (unsigned long)len);
  free(text);
  if (rc != 0) {
    zyxmysql_fail_stmt(prepared->stmt, "the statement could not be prepared");
    mysql_stmt_close(prepared->stmt);
    free(prepared);
    return NULL;
  }
  prepared->param_count = mysql_stmt_param_count(prepared->stmt);
  if (prepared->param_count > 0) {
    prepared->params = (struct zyxmysql_value *)calloc(prepared->param_count, sizeof(*prepared->params));
    if (prepared->params == NULL) {
      mysql_stmt_close(prepared->stmt);
      free(prepared);
      zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
      return NULL;
    }
  }
  return prepared;
}

int32_t zyxmysql_param_count(void *handle) {
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)handle;
  return (int32_t)prepared->param_count;
}

/* zyxmysql_param answers the place a parameter setting writes, or nothing
   when the index is not one of the statement's parameters. */
static struct zyxmysql_value *zyxmysql_param(void *handle, int32_t index) {
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)handle;
  if (index < 0 || (unsigned long)index >= prepared->param_count) {
    zyxmysql_fail(ZYXMYSQL_INVALID, 0, "the parameter is not one of the statement's");
    return NULL;
  }
  return &prepared->params[index];
}

int32_t zyxmysql_bind_null(void *handle, int32_t index) {
  struct zyxmysql_value *value = zyxmysql_param(handle, index);
  if (value == NULL) return -1;
  value->kind = 0;
  return 0;
}

int32_t zyxmysql_bind_int(void *handle, int32_t index, int64_t v) {
  struct zyxmysql_value *value = zyxmysql_param(handle, index);
  if (value == NULL) return -1;
  value->kind = 1;
  value->integer = v;
  return 0;
}

int32_t zyxmysql_bind_float(void *handle, int32_t index, double v) {
  struct zyxmysql_value *value = zyxmysql_param(handle, index);
  if (value == NULL) return -1;
  value->kind = 2;
  value->floating = v;
  return 0;
}

int32_t zyxmysql_bind_text(void *handle, int32_t index, const char *text, size_t len) {
  struct zyxmysql_value *value = zyxmysql_param(handle, index);
  if (value == NULL) return -1;
  char *copy = zyxmysql_owned(text, len);
  if (copy == NULL) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return -1;
  }
  free(value->text);
  value->kind = 3;
  value->text = copy;
  value->len = len;
  return 0;
}

/* zyxmysql_param_binds builds the array the prepared statement's parameter
   call takes. The values it points at belong to the statement and live until
   the statement is closed. */
static MYSQL_BIND *zyxmysql_param_binds(struct zyxmysql_stmt *prepared) {
  MYSQL_BIND *binds = (MYSQL_BIND *)calloc(prepared->param_count, sizeof(*binds));
  if (binds == NULL) return NULL;
  unsigned long i = 0;
  for (i = 0; i < prepared->param_count; i++) {
    struct zyxmysql_value *value = &prepared->params[i];
    switch (value->kind) {
      case 0:
        binds[i].buffer_type = MYSQL_TYPE_NULL;
        break;
      case 1:
        binds[i].buffer_type = MYSQL_TYPE_LONGLONG;
        binds[i].buffer = &value->integer;
        binds[i].is_unsigned = 0;
        break;
      case 2:
        binds[i].buffer_type = MYSQL_TYPE_DOUBLE;
        binds[i].buffer = &value->floating;
        break;
      default:
        value->bound = (unsigned long)value->len;
        binds[i].buffer_type = MYSQL_TYPE_STRING;
        binds[i].buffer = value->text;
        binds[i].buffer_length = (unsigned long)value->len;
        binds[i].length = &value->bound;
        break;
    }
  }
  return binds;
}

int64_t zyxmysql_execute(void *handle) {
  zyxmysql_clear();
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)handle;
  if (prepared->param_count > 0) {
    MYSQL_BIND *binds = zyxmysql_param_binds(prepared);
    if (binds == NULL) {
      zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
      return -1;
    }
    if (mysql_stmt_bind_param(prepared->stmt, binds) != 0) {
      zyxmysql_fail_stmt(prepared->stmt, "the parameters could not be set");
      free(binds);
      return -1;
    }
    free(binds);
  }
  if (mysql_stmt_execute(prepared->stmt) != 0) {
    zyxmysql_fail_stmt(prepared->stmt, "the statement could not be run");
    return -1;
  }
  return (int64_t)mysql_stmt_affected_rows(prepared->stmt);
}

/* zyxmysql_release gives up the wrapper of a statement, keeping the statement
   itself. */
static void zyxmysql_release(struct zyxmysql_stmt *prepared) {
  if (prepared->params != NULL) {
    unsigned long i = 0;
    for (i = 0; i < prepared->param_count; i++) free(prepared->params[i].text);
    free(prepared->params);
  }
  free(prepared);
}

void *zyxmysql_result(void *handle) {
  zyxmysql_clear();
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)handle;
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)calloc(1, sizeof(*rows));
  if (rows == NULL) {
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  /* The rows take the statement over, so the wrapper is never touched again
     and the statement is closed with the rows. */
  rows->kind = 1;
  rows->stmt = prepared->stmt;
  prepared->stmt = NULL;
  zyxmysql_release(prepared);
  MYSQL_RES *meta = mysql_stmt_result_metadata(rows->stmt);
  if (meta == NULL) {
    if (mysql_stmt_field_count(rows->stmt) == 0) return rows;
    zyxmysql_fail_stmt(rows->stmt, "the result could not be read");
    mysql_stmt_close(rows->stmt);
    free(rows);
    return NULL;
  }
  rows->res = meta;
  rows->nfields = mysql_num_fields(meta);
  rows->fields = mysql_fetch_fields(meta);
  if (mysql_stmt_store_result(rows->stmt) != 0) {
    zyxmysql_fail_stmt(rows->stmt, "the result could not be read");
    mysql_free_result(rows->res);
    mysql_stmt_close(rows->stmt);
    free(rows);
    return NULL;
  }
  rows->binds = (MYSQL_BIND *)calloc(rows->nfields, sizeof(*rows->binds));
  rows->data = (char **)calloc(rows->nfields, sizeof(*rows->data));
  rows->cap = (size_t *)calloc(rows->nfields, sizeof(*rows->cap));
  rows->is_null = (my_bool *)calloc(rows->nfields, sizeof(*rows->is_null));
  rows->error = (my_bool *)calloc(rows->nfields, sizeof(*rows->error));
  rows->lens = (unsigned long *)calloc(rows->nfields, sizeof(*rows->lens));
  if (rows->binds == NULL || rows->data == NULL || rows->cap == NULL ||
      rows->is_null == NULL || rows->error == NULL || rows->lens == NULL) {
    zyxmysql_rows_close(rows);
    zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
    return NULL;
  }
  unsigned int i = 0;
  for (i = 0; i < rows->nfields; i++) {
    /* A column that holds no table value, such as a count, reports no width,
       so a width is chosen for it and grown when a value does not fit. */
    size_t width = rows->fields[i].length;
    if (width == 0) width = 64;
    rows->cap[i] = width + 1;
    rows->data[i] = (char *)malloc(rows->cap[i]);
    if (rows->data[i] == NULL) {
      zyxmysql_rows_close(rows);
      zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
      return NULL;
    }
    /* Every column crosses as a string, so one read serves every kind and a
       value of any width reads back exactly. */
    rows->binds[i].buffer_type = MYSQL_TYPE_STRING;
    rows->binds[i].buffer = rows->data[i];
    rows->binds[i].buffer_length = (unsigned long)rows->cap[i];
    rows->binds[i].is_null = &rows->is_null[i];
    rows->binds[i].error = &rows->error[i];
    rows->binds[i].length = &rows->lens[i];
  }
  if (mysql_stmt_bind_result(rows->stmt, rows->binds) != 0) {
    zyxmysql_fail_stmt(rows->stmt, "the result columns could not be read");
    zyxmysql_rows_close(rows);
    return NULL;
  }
  return rows;
}

void zyxmysql_stmt_close(void *handle) {
  if (handle == NULL) return;
  struct zyxmysql_stmt *prepared = (struct zyxmysql_stmt *)handle;
  if (prepared->stmt != NULL) mysql_stmt_close(prepared->stmt);
  zyxmysql_release(prepared);
}

int32_t zyxmysql_rows_next(void *handle) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  if (rows->done) return 0;
  if (rows->kind == 0) {
    if (rows->res == NULL) {
      rows->done = 1;
      rows->current = 0;
      return 0;
    }
    rows->row = mysql_fetch_row(rows->res);
    if (rows->row == NULL) {
      rows->done = 1;
      rows->current = 0;
      if (mysql_errno(rows->conn) != 0) {
        zyxmysql_fail_conn(rows->conn, "the row could not be read");
        return -1;
      }
      return 0;
    }
    rows->lengths = mysql_fetch_lengths(rows->res);
    rows->current = 1;
    return 1;
  }
  int rc = mysql_stmt_fetch(rows->stmt);
  if (rc == MYSQL_NO_DATA) {
    rows->done = 1;
    rows->current = 0;
    return 0;
  }
  if (rc != 0 && rc != MYSQL_DATA_TRUNCATED) {
    zyxmysql_fail_stmt(rows->stmt, "the row could not be read");
    rows->done = 1;
    rows->current = 0;
    return -1;
  }
  unsigned int i = 0;
  for (i = 0; i < rows->nfields; i++) {
    if (rows->lens[i] >= rows->cap[i]) {
      /* The value did not fit the buffer chosen from the column's own width,
         so a buffer of its exact width takes it and the column is read
         again. */
      char *bigger = (char *)realloc(rows->data[i], rows->lens[i] + 1);
      if (bigger == NULL) {
        zyxmysql_fail(ZYXMYSQL_OTHER, 0, "out of memory");
        rows->done = 1;
        rows->current = 0;
        return -1;
      }
      rows->data[i] = bigger;
      rows->cap[i] = rows->lens[i] + 1;
      rows->binds[i].buffer = bigger;
      rows->binds[i].buffer_length = (unsigned long)rows->cap[i];
      if (mysql_stmt_fetch_column(rows->stmt, &rows->binds[i], i, 0) != 0) {
        zyxmysql_fail_stmt(rows->stmt, "the value could not be read");
        rows->done = 1;
        rows->current = 0;
        return -1;
      }
    }
  }
  rows->current = 1;
  return 1;
}

int32_t zyxmysql_rows_columns(void *handle) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  return (int32_t)rows->nfields;
}

void zyxmysql_rows_column_name(void *handle, int32_t index, void *buf) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  if (index < 0 || (unsigned int)index >= rows->nfields) return;
  const char *name = rows->fields[index].name;
  if (name == NULL) return;
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
}

/* zyxmysql_value_bytes answers the bytes of the current row's column, or
   nothing when the column holds no value. */
static const char *zyxmysql_value_bytes(struct zyxmysql_rows *rows, int32_t index, size_t *len) {
  *len = 0;
  if (index < 0 || (unsigned int)index >= rows->nfields) return NULL;
  if (rows->kind == 0) {
    if (rows->row == NULL || rows->row[index] == NULL) return NULL;
    *len = rows->lengths != NULL ? (size_t)rows->lengths[index] : strlen(rows->row[index]);
    return rows->row[index];
  }
  if (rows->is_null[index]) return NULL;
  *len = (size_t)rows->lens[index];
  return rows->data[index];
}

int32_t zyxmysql_rows_null(void *handle, int32_t index) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  size_t len = 0;
  return zyxmysql_value_bytes(rows, index, &len) == NULL ? 1 : 0;
}

int64_t zyxmysql_rows_int(void *handle, int32_t index) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxmysql_value_bytes(rows, index, &len);
  if (bytes == NULL) return 0;
  char small[64];
  char *text = small;
  if (len + 1 > sizeof(small)) {
    text = (char *)malloc(len + 1);
    if (text == NULL) return 0;
  }
  memcpy(text, bytes, len);
  text[len] = 0;
  int64_t value = strtoll(text, NULL, 10);
  if (text != small) free(text);
  return value;
}

double zyxmysql_rows_float(void *handle, int32_t index) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxmysql_value_bytes(rows, index, &len);
  if (bytes == NULL) return 0.0;
  char small[64];
  char *text = small;
  if (len + 1 > sizeof(small)) {
    text = (char *)malloc(len + 1);
    if (text == NULL) return 0.0;
  }
  memcpy(text, bytes, len);
  text[len] = 0;
  double value = strtod(text, NULL);
  if (text != small) free(text);
  return value;
}

void zyxmysql_rows_text(void *handle, int32_t index, void *buf) {
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxmysql_value_bytes(rows, index, &len);
  if (bytes == NULL || len == 0) return;
  zyxrt_buf_write(buf, (const unsigned char *)bytes, len);
}

void zyxmysql_rows_close(void *handle) {
  if (handle == NULL) return;
  struct zyxmysql_rows *rows = (struct zyxmysql_rows *)handle;
  if (rows->res != NULL) mysql_free_result(rows->res);
  if (rows->kind == 1 && rows->stmt != NULL) {
    unsigned int i = 0;
    for (i = 0; i < rows->nfields; i++) free(rows->data != NULL ? rows->data[i] : NULL);
    free(rows->data);
    free(rows->cap);
    free(rows->is_null);
    free(rows->error);
    free(rows->lens);
    free(rows->binds);
    mysql_stmt_free_result(rows->stmt);
    mysql_stmt_close(rows->stmt);
  }
  free(rows);
}
