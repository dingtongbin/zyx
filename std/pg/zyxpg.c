#include "zyxpg.h"

#include "zyxrt.h"

#include <libpq-fe.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* The failure state of the calling thread, in the shape every system package
   reports one. */
static _Thread_local int32_t zyxpg_kind;
static _Thread_local int32_t zyxpg_code;
static _Thread_local char zyxpg_text[512];

static void zyxpg_clear(void) {
  zyxpg_kind = 0;
  zyxpg_code = 0;
  zyxpg_text[0] = 0;
}

static void zyxpg_fail(int32_t kind, int32_t code, const char *text) {
  zyxpg_kind = kind;
  zyxpg_code = code;
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  while (n > 0 && (text[n - 1] == '\n' || text[n - 1] == '\r')) n--;
  if (n >= sizeof(zyxpg_text)) n = sizeof(zyxpg_text) - 1;
  memcpy(zyxpg_text, text, n);
  zyxpg_text[n] = 0;
}

/* zyxpg_has reports whether a text holds a substring, comparing without
   regard to the case of an ASCII letter. */
static int zyxpg_has(const char *text, const char *sub) {
  size_t n = strlen(sub);
  for (; *text != 0; text++) {
    size_t i = 0;
    while (i < n) {
      char a = text[i];
      char b = sub[i];
      if (a >= 'A' && a <= 'Z') a = (char)(a - 'A' + 'a');
      if (b >= 'A' && b <= 'Z') b = (char)(b - 'A' + 'a');
      if (a != b) break;
      i++;
    }
    if (i == n) return 1;
  }
  return 0;
}

/* zyxpg_kind_of_sqlstate maps the five-character state a server error carries
   onto the kind of failure it is. */
static int32_t zyxpg_kind_of_sqlstate(const char *state) {
  if (state == NULL) return ZYXPG_OTHER;
  if (strncmp(state, "23505", 5) == 0) return ZYXPG_EXISTS;
  if (strncmp(state, "23503", 5) == 0) return ZYXPG_EXISTS;
  if (strncmp(state, "23502", 5) == 0) return ZYXPG_INVALID;
  if (strncmp(state, "23514", 5) == 0) return ZYXPG_INVALID;
  if (strncmp(state, "23", 2) == 0) return ZYXPG_INVALID;
  if (strncmp(state, "28", 2) == 0) return ZYXPG_PERMISSION;
  if (strncmp(state, "3D000", 5) == 0) return ZYXPG_NOT_FOUND;
  if (strncmp(state, "42P01", 5) == 0) return ZYXPG_NOT_FOUND;
  if (strncmp(state, "42", 2) == 0) return ZYXPG_INVALID;
  if (strncmp(state, "40", 2) == 0) return ZYXPG_WOULD_BLOCK;
  if (strncmp(state, "57014", 5) == 0) return ZYXPG_INTERRUPTED;
  if (strncmp(state, "57", 2) == 0) return ZYXPG_RESET;
  if (strncmp(state, "08", 2) == 0) return ZYXPG_RESET;
  return ZYXPG_OTHER;
}

/* zyxpg_kind_of_message maps a connection failure onto the kind of failure it
   is. libpq names no number for these, so the text it wrote is read. */
static int32_t zyxpg_kind_of_message(const char *text) {
  if (text == NULL) return ZYXPG_OTHER;
  if (zyxpg_has(text, "refused")) return ZYXPG_REFUSED;
  if (zyxpg_has(text, "timeout")) return ZYXPG_TIMEOUT;
  if (zyxpg_has(text, "could not translate host name")) return ZYXPG_UNREACHABLE;
  if (zyxpg_has(text, "no such host")) return ZYXPG_UNREACHABLE;
  if (zyxpg_has(text, "unknown host")) return ZYXPG_UNREACHABLE;
  if (zyxpg_has(text, "authentication")) return ZYXPG_PERMISSION;
  if (zyxpg_has(text, "password")) return ZYXPG_PERMISSION;
  if (zyxpg_has(text, "does not exist")) return ZYXPG_NOT_FOUND;
  return ZYXPG_OTHER;
}

/* zyxpg_fail_conn records the failure a connection reported. */
static void zyxpg_fail_conn(PGconn *conn, const char *fallback) {
  const char *text = conn != NULL ? PQerrorMessage(conn) : NULL;
  if (text == NULL || text[0] == 0) {
    zyxpg_fail(ZYXPG_OTHER, 0, fallback);
    return;
  }
  zyxpg_fail(zyxpg_kind_of_message(text), 0, text);
}

/* zyxpg_status_ok reports whether a result carries what the statement asked
   for rather than a failure. */
static int zyxpg_status_ok(PGresult *res) {
  ExecStatusType status = PQresultStatus(res);
  return status == PGRES_TUPLES_OK || status == PGRES_COMMAND_OK || status == PGRES_EMPTY_QUERY;
}

/* zyxpg_fail_result records the failure a result carries, which is the state
   of the server when it names one. */
static void zyxpg_fail_result(PGresult *res, PGconn *conn) {
  const char *state = PQresultErrorField(res, PG_DIAG_SQLSTATE);
  const char *text = PQresultErrorMessage(res);
  if (state != NULL && state[0] != 0) {
    zyxpg_fail(zyxpg_kind_of_sqlstate(state), 0, text);
    return;
  }
  zyxpg_fail_conn(conn, text != NULL ? text : "the statement could not be run");
}

/* zyxpg_owned copies the bytes of a call's argument into a string that ends
   with a zero byte, because a text of the language is a view without one. */
static char *zyxpg_owned(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, text, len);
  out[len] = 0;
  return out;
}

int32_t zyxpg_error_kind(void) { return zyxpg_kind; }

int32_t zyxpg_error_number(void) { return zyxpg_code; }

void zyxpg_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxpg_text, strlen(zyxpg_text));
}

void *zyxpg_connect(const char *dsn, size_t len) {
  zyxpg_clear();
  char *text = zyxpg_owned(dsn, len);
  if (text == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return NULL;
  }
  PGconn *conn = PQconnectdb(text);
  free(text);
  if (conn == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return NULL;
  }
  if (PQstatus(conn) != CONNECTION_OK) {
    zyxpg_fail_conn(conn, "the connection could not be made");
    PQfinish(conn);
    return NULL;
  }
  return conn;
}

void zyxpg_close(void *conn) {
  if (conn == NULL) return;
  PQfinish((PGconn *)conn);
}

/* struct zyxpg_params is the parameters of one run: each is a text the client
   library reads, and a parameter that holds no value is a null pointer. */
struct zyxpg_params {
  int count;
  int cap;
  char **values;
  int *lengths;
};
#define ZYXPG_FORMAT_TEXT 0

static int zyxpg_params_grow(struct zyxpg_params *params) {
  if (params->count < params->cap) return 1;
  int cap = params->cap == 0 ? 4 : params->cap * 2;
  char **values = (char **)realloc(params->values, (size_t)cap * sizeof(*values));
  if (values == NULL) return 0;
  params->values = values;
  int *lengths = (int *)realloc(params->lengths, (size_t)cap * sizeof(*lengths));
  if (lengths == NULL) return 0;
  params->lengths = lengths;
  params->cap = cap;
  return 1;
}

void *zyxpg_params_new(void) {
  struct zyxpg_params *params = (struct zyxpg_params *)calloc(1, sizeof(*params));
  return params;
}

void zyxpg_params_free(void *handle) {
  if (handle == NULL) return;
  struct zyxpg_params *params = (struct zyxpg_params *)handle;
  int i = 0;
  for (i = 0; i < params->count; i++) free(params->values[i]);
  free(params->values);
  free(params->lengths);
  free(params);
}

static int32_t zyxpg_params_put(struct zyxpg_params *params, char *text, int len) {
  if (!zyxpg_params_grow(params)) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return -1;
  }
  params->values[params->count] = text;
  params->lengths[params->count] = len;
  params->count += 1;
  return 0;
}

int32_t zyxpg_params_null(void *handle) {
  return zyxpg_params_put((struct zyxpg_params *)handle, NULL, 0);
}

int32_t zyxpg_params_bool(void *handle, int32_t value) {
  char *text = zyxpg_owned(value ? "true" : "false", value ? 4 : 5);
  if (text == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return -1;
  }
  return zyxpg_params_put((struct zyxpg_params *)handle, text, (int)strlen(text));
}

int32_t zyxpg_params_float(void *handle, double value) {
  char buffer[32];
  snprintf(buffer, sizeof(buffer), "%.17g", value);
  char *text = zyxpg_owned(buffer, strlen(buffer));
  if (text == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return -1;
  }
  return zyxpg_params_put((struct zyxpg_params *)handle, text, (int)strlen(text));
}

int32_t zyxpg_params_text(void *handle, const char *text, size_t len) {
  char *copy = zyxpg_owned(text, len);
  if (copy == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return -1;
  }
  return zyxpg_params_put((struct zyxpg_params *)handle, copy, (int)len);
}

/* zyxpg_run runs a text with the parameters of a call and answers the result.
   It answers nothing on a failure and leaves the state behind. */
static PGresult *zyxpg_run(PGconn *conn, const char *sql, size_t len, void *handle) {
  char *text = zyxpg_owned(sql, len);
  if (text == NULL) {
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return NULL;
  }
  struct zyxpg_params *params = (struct zyxpg_params *)handle;
  PGresult *res = NULL;
  if (params != NULL && params->count > 0) {
    res = PQexecParams(conn, text, params->count, NULL,
                       (const char *const *)params->values, params->lengths, NULL,
                       ZYXPG_FORMAT_TEXT);
  } else {
    res = PQexec(conn, text);
  }
  free(text);
  if (res == NULL) zyxpg_fail_conn(conn, "the statement could not be run");
  return res;
}

/* struct zyxpg_rows is the rows of one result. */
struct zyxpg_rows {
  PGresult *res;
  int at;
};

void *zyxpg_query(void *conn_handle, const char *sql, size_t len, void *params) {
  zyxpg_clear();
  PGconn *conn = (PGconn *)conn_handle;
  PGresult *res = zyxpg_run(conn, sql, len, params);
  if (res == NULL) return NULL;
  if (!zyxpg_status_ok(res)) {
    zyxpg_fail_result(res, conn);
    PQclear(res);
    return NULL;
  }
  struct zyxpg_rows *rows = (struct zyxpg_rows *)calloc(1, sizeof(*rows));
  if (rows == NULL) {
    PQclear(res);
    zyxpg_fail(ZYXPG_OTHER, 0, "out of memory");
    return NULL;
  }
  rows->res = res;
  rows->at = -1;
  return rows;
}

int64_t zyxpg_exec(void *conn_handle, const char *sql, size_t len, void *params) {
  zyxpg_clear();
  PGconn *conn = (PGconn *)conn_handle;
  PGresult *res = zyxpg_run(conn, sql, len, params);
  if (res == NULL) return -1;
  if (!zyxpg_status_ok(res)) {
    zyxpg_fail_result(res, conn);
    PQclear(res);
    return -1;
  }
  const char *tuples = PQcmdTuples(res);
  int64_t affected = 0;
  if (tuples != NULL && tuples[0] != 0) affected = strtoll(tuples, NULL, 10);
  PQclear(res);
  return affected;
}

int32_t zyxpg_rows_next(void *handle) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  if (rows->at + 1 >= PQntuples(rows->res)) return 0;
  rows->at += 1;
  return 1;
}

int32_t zyxpg_rows_columns(void *handle) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  return (int32_t)PQnfields(rows->res);
}

void zyxpg_rows_column_name(void *handle, int32_t index, void *buf) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  if (index < 0 || index >= PQnfields(rows->res)) return;
  const char *name = PQfname(rows->res, index);
  if (name == NULL) return;
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
}

/* zyxpg_value answers the bytes of the current row's column, or nothing when
   the column holds no value. */
static const char *zyxpg_value(struct zyxpg_rows *rows, int32_t index, size_t *len) {
  *len = 0;
  if (rows->at < 0 || index < 0 || index >= PQnfields(rows->res)) return NULL;
  if (PQgetisnull(rows->res, rows->at, index)) return NULL;
  *len = (size_t)PQgetlength(rows->res, rows->at, index);
  return PQgetvalue(rows->res, rows->at, index);
}

int32_t zyxpg_rows_null(void *handle, int32_t index) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  size_t len = 0;
  return zyxpg_value(rows, index, &len) == NULL ? 1 : 0;
}

int64_t zyxpg_rows_int(void *handle, int32_t index) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxpg_value(rows, index, &len);
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

double zyxpg_rows_float(void *handle, int32_t index) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxpg_value(rows, index, &len);
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

void zyxpg_rows_text(void *handle, int32_t index, void *buf) {
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  size_t len = 0;
  const char *bytes = zyxpg_value(rows, index, &len);
  if (bytes == NULL || len == 0) return;
  zyxrt_buf_write(buf, (const unsigned char *)bytes, len);
}

void zyxpg_rows_close(void *handle) {
  if (handle == NULL) return;
  struct zyxpg_rows *rows = (struct zyxpg_rows *)handle;
  PQclear(rows->res);
  free(rows);
}
