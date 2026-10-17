#include "zyxsqlite.h"

#include "zyxrt.h"

#include <sqlite3.h>
#include <stdlib.h>
#include <string.h>

/* The failure state of the calling thread, in the shape every system package
   reports one. A call of this package runs on the thread of the coroutine
   that made it, so the state is read back by the call after it. */
static _Thread_local int32_t zyxsqlite_kind;
static _Thread_local int32_t zyxsqlite_code;
static _Thread_local char zyxsqlite_text[512];

static void zyxsqlite_clear(void) {
  zyxsqlite_kind = 0;
  zyxsqlite_code = 0;
  zyxsqlite_text[0] = 0;
}

static void zyxsqlite_fail(int32_t kind, int32_t code, const char *text) {
  zyxsqlite_kind = kind;
  zyxsqlite_code = code;
  if (text == NULL) text = "unknown error";
  size_t n = strlen(text);
  if (n >= sizeof(zyxsqlite_text)) n = sizeof(zyxsqlite_text) - 1;
  memcpy(zyxsqlite_text, text, n);
  zyxsqlite_text[n] = 0;
}

/* zyxsqlite_kind_of maps a SQLite result code onto the kind of failure it is.
   The extended codes carry the primary one in their low byte, so one case of
   the switch serves them all. */
static int32_t zyxsqlite_kind_of(int32_t code) {
  switch (code & 0xff) {
    case SQLITE_PERM:
    case SQLITE_READONLY:
    case SQLITE_AUTH:
      return ZYXSQLITE_PERMISSION;
    case SQLITE_CANTOPEN:
    case SQLITE_NOTFOUND:
      return ZYXSQLITE_NOT_FOUND;
    case SQLITE_BUSY:
    case SQLITE_LOCKED:
      return ZYXSQLITE_WOULD_BLOCK;
    case SQLITE_INTERRUPT:
      return ZYXSQLITE_INTERRUPTED;
    case SQLITE_ERROR:
    case SQLITE_CORRUPT:
    case SQLITE_NOTADB:
    case SQLITE_TOOBIG:
    case SQLITE_CONSTRAINT:
    case SQLITE_MISUSE:
    case SQLITE_RANGE:
      return ZYXSQLITE_INVALID;
    default:
      return ZYXSQLITE_OTHER;
  }
}

/* zyxsqlite_fail_db records the failure a connection reported: the number is
   the extended one, which names the constraint that was broken, and the text
   is the one SQLite keeps for the connection. */
static void zyxsqlite_fail_db(sqlite3 *db, int32_t code) {
  if (db == NULL) {
    zyxsqlite_fail(zyxsqlite_kind_of(code), code, sqlite3_errstr(code));
    return;
  }
  int32_t extended = sqlite3_extended_errcode(db);
  if (extended == SQLITE_OK) extended = code;
  zyxsqlite_fail(zyxsqlite_kind_of(extended), extended, sqlite3_errmsg(db));
}

/* zyxsqlite_fail_stmt records the failure of a call that was handed a
   statement. A call whose connection has nothing to add reports the code it
   answered itself, which is what a binding call out of range does. */
static void zyxsqlite_fail_stmt(void *stmt, int32_t code) {
  sqlite3 *db = sqlite3_db_handle((sqlite3_stmt *)stmt);
  int32_t extended = db != NULL ? sqlite3_extended_errcode(db) : SQLITE_OK;
  if (extended != SQLITE_OK && (extended & 0xff) == (code & 0xff)) {
    zyxsqlite_fail_db(db, code);
    return;
  }
  zyxsqlite_fail(zyxsqlite_kind_of(code), code, sqlite3_errstr(code));
}

/* zyxsqlite_owned copies the bytes of a call's argument into a string that
   ends with a zero byte, because a text of the language is a view without
   one. */
static char *zyxsqlite_owned(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, text, len);
  out[len] = 0;
  return out;
}

int32_t zyxsqlite_error_kind(void) { return zyxsqlite_kind; }

int32_t zyxsqlite_error_number(void) { return zyxsqlite_code; }

void zyxsqlite_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxsqlite_text, strlen(zyxsqlite_text));
}

void *zyxsqlite_open(const char *path, size_t len) {
  zyxsqlite_clear();
  char *name = zyxsqlite_owned(path, len);
  if (name == NULL) {
    zyxsqlite_fail(ZYXSQLITE_OTHER, 0, "out of memory");
    return NULL;
  }
  sqlite3 *db = NULL;
  int rc = sqlite3_open_v2(name, &db, SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE, NULL);
  free(name);
  if (rc != SQLITE_OK) {
    /* A connection SQLite made and then refused is one it still holds, so it
       is given up here rather than left behind: the failure is read first. */
    zyxsqlite_fail_db(db, rc);
    if (db != NULL) sqlite3_close_v2(db);
    return NULL;
  }
  return db;
}

int32_t zyxsqlite_close(void *handle) {
  zyxsqlite_clear();
  if (handle == NULL) return 0;
  sqlite3 *db = (sqlite3 *)handle;
  int rc = sqlite3_close_v2(db);
  if (rc != SQLITE_OK) {
    zyxsqlite_fail_db(db, rc);
    return -1;
  }
  return 0;
}

/* zyxsqlite_rest_is_empty reports whether what follows a compiled statement
   holds anything but the whitespace, the semicolons, and the comments that
   say nothing. */
static int zyxsqlite_rest_is_empty(const char *text) {
  const char *at = text;
  for (;;) {
    if (*at == 0) return 1;
    if (*at == ';' || (*at >= 0x09 && *at <= 0x0d) || *at == ' ') {
      at++;
      continue;
    }
    if (at[0] == '-' && at[1] == '-') {
      at += 2;
      while (*at != 0 && *at != '\n') at++;
      continue;
    }
    if (at[0] == '/' && at[1] == '*') {
      at += 2;
      while (*at != 0 && !(at[0] == '*' && at[1] == '/')) at++;
      if (*at == 0) return 1;
      at += 2;
      continue;
    }
    return 0;
  }
}

void *zyxsqlite_prepare(void *handle, const char *sql, size_t len) {
  zyxsqlite_clear();
  sqlite3 *db = (sqlite3 *)handle;
  char *text = zyxsqlite_owned(sql, len);
  if (text == NULL) {
    zyxsqlite_fail(ZYXSQLITE_OTHER, 0, "out of memory");
    return NULL;
  }
  sqlite3_stmt *stmt = NULL;
  const char *tail = text;
  int rc = sqlite3_prepare_v2(db, text, -1, &stmt, &tail);
  if (rc != SQLITE_OK) {
    zyxsqlite_fail_db(db, rc);
    free(text);
    return NULL;
  }
  if (stmt == NULL) {
    zyxsqlite_fail(ZYXSQLITE_INVALID, 0, "the text holds no statement");
    free(text);
    return NULL;
  }
  /* A second statement is refused rather than left behind, because a caller
     that wrote one meant to run it. */
  if (!zyxsqlite_rest_is_empty(tail)) {
    zyxsqlite_fail(ZYXSQLITE_INVALID, 0, "the text holds more than one statement");
    sqlite3_finalize(stmt);
    free(text);
    return NULL;
  }
  free(text);
  return stmt;
}

void zyxsqlite_finalize(void *stmt) {
  if (stmt == NULL) return;
  sqlite3_finalize((sqlite3_stmt *)stmt);
}

int32_t zyxsqlite_step(void *stmt) {
  zyxsqlite_clear();
  sqlite3_stmt *statement = (sqlite3_stmt *)stmt;
  int rc = sqlite3_step(statement);
  if (rc == SQLITE_ROW) return 1;
  if (rc == SQLITE_DONE) return 0;
  zyxsqlite_fail_stmt(statement, rc);
  return -1;
}

/* zyxsqlite_bound reads the answer of one binding call. */
static int32_t zyxsqlite_bound(void *stmt, int rc) {
  if (rc == SQLITE_OK) return 0;
  zyxsqlite_fail_stmt(stmt, rc);
  return -1;
}

int32_t zyxsqlite_bind_null(void *stmt, int32_t index) {
  return zyxsqlite_bound(stmt, sqlite3_bind_null((sqlite3_stmt *)stmt, index));
}

int32_t zyxsqlite_bind_int(void *stmt, int32_t index, int64_t value) {
  return zyxsqlite_bound(stmt, sqlite3_bind_int64((sqlite3_stmt *)stmt, index, (sqlite3_int64)value));
}

int32_t zyxsqlite_bind_float(void *stmt, int32_t index, double value) {
  return zyxsqlite_bound(stmt, sqlite3_bind_double((sqlite3_stmt *)stmt, index, value));
}

int32_t zyxsqlite_bind_text(void *stmt, int32_t index, const char *text, size_t len) {
  return zyxsqlite_bound(stmt, sqlite3_bind_text((sqlite3_stmt *)stmt, index, text, (int)len, SQLITE_TRANSIENT));
}

int64_t zyxsqlite_changes(void *handle) {
  if (handle == NULL) return 0;
  return (int64_t)sqlite3_changes((sqlite3 *)handle);
}

int32_t zyxsqlite_column_count(void *stmt) {
  if (stmt == NULL) return 0;
  return sqlite3_column_count((sqlite3_stmt *)stmt);
}

void zyxsqlite_column_name(void *stmt, int32_t index, void *buf) {
  if (stmt == NULL) return;
  const char *name = sqlite3_column_name((sqlite3_stmt *)stmt, index);
  if (name == NULL) return;
  zyxrt_buf_write(buf, (const unsigned char *)name, strlen(name));
}

int32_t zyxsqlite_column_null(void *stmt, int32_t index) {
  if (stmt == NULL) return 1;
  return sqlite3_column_type((sqlite3_stmt *)stmt, index) == SQLITE_NULL ? 1 : 0;
}

int64_t zyxsqlite_column_int(void *stmt, int32_t index) {
  if (stmt == NULL) return 0;
  return (int64_t)sqlite3_column_int64((sqlite3_stmt *)stmt, index);
}

double zyxsqlite_column_float(void *stmt, int32_t index) {
  if (stmt == NULL) return 0.0;
  return sqlite3_column_double((sqlite3_stmt *)stmt, index);
}

void zyxsqlite_column_text(void *stmt, int32_t index, void *buf) {
  if (stmt == NULL) return;
  sqlite3_stmt *statement = (sqlite3_stmt *)stmt;
  const unsigned char *text = sqlite3_column_text(statement, index);
  if (text == NULL) return;
  /* The byte count is read after the text, because the call that makes the
     text may change what the count was. */
  int n = sqlite3_column_bytes(statement, index);
  if (n <= 0) return;
  zyxrt_buf_write(buf, text, (size_t)n);
}
