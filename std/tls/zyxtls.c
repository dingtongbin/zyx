#include "zyxtls.h"

#include "zyxrt.h"

#include <openssl/err.h>
#include <openssl/ssl.h>
#include <openssl/tls1.h>
#include <openssl/x509_vfy.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* The failure state of the calling thread, in the shape every system package
   reports one. */
static _Thread_local int32_t zyxtls_kind;
static _Thread_local int32_t zyxtls_number;
static _Thread_local char zyxtls_text[512];

static void zyxtls_clear(void) {
  zyxtls_kind = 0;
  zyxtls_number = 0;
  zyxtls_text[0] = 0;
}

static void zyxtls_fail(int32_t kind, int32_t number, const char *text) {
  zyxtls_kind = kind;
  zyxtls_number = number;
  if (text == NULL) text = "unknown failure";
  size_t n = strlen(text);
  if (n >= sizeof(zyxtls_text)) n = sizeof(zyxtls_text) - 1;
  memcpy(zyxtls_text, text, n);
  zyxtls_text[n] = 0;
}

/* zyxtls_kind_of_errno maps the number the system gave onto the kind of
   failure it is. */
static int32_t zyxtls_kind_of_errno(int number) {
  switch (number) {
    case ENOENT:
      return ZYXTLS_NOT_FOUND;
    case EACCES:
      return ZYXTLS_PERMISSION;
    default:
      return ZYXTLS_OTHER;
  }
}

/* zyxtls_fail_openssl records the failure OpenSSL left behind. The check of
   the certificate of the peer comes first, because a handshake that refused a
   certificate leaves both a check result and an error of its own. A failure
   of the system is the root of what went wrong when the queue holds one, and
   it carries the number of the system; any other one carries no number,
   because the codes OpenSSL packs are its own business. */
static void zyxtls_fail_openssl(void *conn, const char *fallback) {
  SSL *ssl = (SSL *)conn;
  if (ssl != NULL) {
    long result = SSL_get_verify_result(ssl);
    if (result != X509_V_OK) {
      zyxtls_fail(ZYXTLS_INVALID, (int32_t)result, X509_verify_cert_error_string(result));
      return;
    }
  }
  unsigned long code, system = 0, seen = 0;
  char text[256];
  text[0] = 0;
  while ((code = ERR_get_error()) != 0) {
    if (seen == 0) {
      ERR_error_string_n(code, text, sizeof(text));
      seen = code;
    }
    if (ERR_GET_LIB(code) == ERR_LIB_SYS) system = code;
  }
  if (system != 0) {
    int number = ERR_GET_REASON(system);
    char whole[256];
    snprintf(whole, sizeof(whole), "%s: %s", fallback, strerror(number));
    zyxtls_fail(zyxtls_kind_of_errno(number), number, whole);
    return;
  }
  if (seen == 0) {
    zyxtls_fail(ZYXTLS_OTHER, 0, fallback);
    return;
  }
  zyxtls_fail(ZYXTLS_OTHER, 0, text);
}

/* zyxtls_unreadable answers the number the system gave when a file cannot be
   read, and zero when it can. A name that is not there and a name the program
   may not read are failures of the system rather than of the library, so they
   are asked for before the library is given the name. */
static int zyxtls_unreadable(const char *path) {
  FILE *file = fopen(path, "rb");
  if (file == NULL) return errno;
  fclose(file);
  return 0;
}

/* zyxtls_owned copies the bytes of a call's argument into a string that ends
   with a zero byte, because a text of the language is a view without one. */
static char *zyxtls_owned(const char *text, size_t len) {
  char *out = (char *)malloc(len + 1);
  if (out == NULL) return NULL;
  memcpy(out, text, len);
  out[len] = 0;
  return out;
}

/* zyxtls_answer turns what OpenSSL reported into the answer of a call. */
static int32_t zyxtls_answer(SSL *ssl, int ret) {
  int error = SSL_get_error(ssl, ret);
  switch (error) {
    case SSL_ERROR_WANT_READ:
      return ZYXTLS_WANT_READ;
    case SSL_ERROR_WANT_WRITE:
      return ZYXTLS_WANT_WRITE;
    case SSL_ERROR_ZERO_RETURN:
      return ZYXTLS_ENDED;
    default:
      zyxtls_fail_openssl(ssl, "the session failed");
      return ZYXTLS_FAILED;
  }
}

int32_t zyxtls_error_kind(void) { return zyxtls_kind; }

int32_t zyxtls_error_number(void) { return zyxtls_number; }

void zyxtls_error_message(void *buf) {
  zyxrt_buf_write(buf, (const unsigned char *)zyxtls_text, strlen(zyxtls_text));
}

void *zyxtls_ctx_new(int32_t server) {
  zyxtls_clear();
  while (ERR_get_error() != 0) {
  }
  SSL_CTX *ctx = SSL_CTX_new(server ? TLS_server_method() : TLS_client_method());
  if (ctx == NULL) {
    zyxtls_fail_openssl(NULL, "no context could be built");
    return NULL;
  }
  /* A client asks for the certificate of the peer and checks it; a server
     asks for none, because the configuration names no certificate of a
     client. */
  SSL_CTX_set_verify(ctx, server ? SSL_VERIFY_NONE : SSL_VERIFY_PEER, NULL);
  return ctx;
}

int32_t zyxtls_ctx_cert(void *handle, const char *path, size_t len) {
  zyxtls_clear();
  SSL_CTX *ctx = (SSL_CTX *)handle;
  char *name = zyxtls_owned(path, len);
  if (name == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return -1;
  }
  int unreadable = zyxtls_unreadable(name);
  if (unreadable != 0) {
    zyxtls_fail(zyxtls_kind_of_errno(unreadable), unreadable, strerror(unreadable));
    free(name);
    return -1;
  }
  int rc = SSL_CTX_use_certificate_chain_file(ctx, name);
  free(name);
  if (rc != 1) {
    zyxtls_fail_openssl(NULL, "the certificate could not be read");
    return -1;
  }
  return 0;
}

int32_t zyxtls_ctx_key(void *handle, const char *path, size_t len) {
  zyxtls_clear();
  SSL_CTX *ctx = (SSL_CTX *)handle;
  char *name = zyxtls_owned(path, len);
  if (name == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return -1;
  }
  int unreadable = zyxtls_unreadable(name);
  if (unreadable != 0) {
    zyxtls_fail(zyxtls_kind_of_errno(unreadable), unreadable, strerror(unreadable));
    free(name);
    return -1;
  }
  int rc = SSL_CTX_use_PrivateKey_file(ctx, name, SSL_FILETYPE_PEM);
  free(name);
  if (rc != 1) {
    zyxtls_fail_openssl(NULL, "the key could not be read");
    return -1;
  }
  if (SSL_CTX_check_private_key(ctx) != 1) {
    zyxtls_fail_openssl(NULL, "the key does not belong to the certificate");
    return -1;
  }
  return 0;
}

int32_t zyxtls_ctx_ca(void *handle, const char *path, size_t len) {
  zyxtls_clear();
  SSL_CTX *ctx = (SSL_CTX *)handle;
  char *name = zyxtls_owned(path, len);
  if (name == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return -1;
  }
  int unreadable = zyxtls_unreadable(name);
  if (unreadable != 0) {
    zyxtls_fail(zyxtls_kind_of_errno(unreadable), unreadable, strerror(unreadable));
    free(name);
    return -1;
  }
  int rc = SSL_CTX_load_verify_locations(ctx, name, NULL);
  free(name);
  if (rc != 1) {
    zyxtls_fail_openssl(NULL, "the certificates to trust could not be read");
    return -1;
  }
  return 0;
}

int32_t zyxtls_ctx_min_version(void *handle, int32_t version) {
  zyxtls_clear();
  SSL_CTX *ctx = (SSL_CTX *)handle;
  int wanted = 0;
  switch (version) {
    case 1:
      wanted = TLS1_2_VERSION;
      break;
    case 2:
      wanted = TLS1_3_VERSION;
      break;
    default:
      zyxtls_fail(ZYXTLS_INVALID, 0, "the version asked for is not one this package speaks");
      return -1;
  }
  if (SSL_CTX_set_min_proto_version(ctx, wanted) != 1) {
    zyxtls_fail_openssl(NULL, "the lowest protocol version could not be set");
    return -1;
  }
  return 0;
}

void zyxtls_ctx_free(void *ctx) {
  if (ctx == NULL) return;
  SSL_CTX_free((SSL_CTX *)ctx);
}

void *zyxtls_conn_new(void *handle, int32_t server) {
  zyxtls_clear();
  while (ERR_get_error() != 0) {
  }
  SSL_CTX *ctx = (SSL_CTX *)handle;
  SSL *ssl = SSL_new(ctx);
  if (ssl == NULL) {
    zyxtls_fail_openssl(NULL, "no session could be built");
    return NULL;
  }
  BIO *incoming = BIO_new(BIO_s_mem());
  BIO *outgoing = BIO_new(BIO_s_mem());
  if (incoming == NULL || outgoing == NULL) {
    if (incoming != NULL) BIO_free(incoming);
    if (outgoing != NULL) BIO_free(outgoing);
    SSL_free(ssl);
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return NULL;
  }
  /* The session owns both buffers from here on. */
  SSL_set_bio(ssl, incoming, outgoing);
  if (server) {
    SSL_set_accept_state(ssl);
  } else {
    SSL_set_connect_state(ssl);
  }
  /* A write may take part of what it was given, and the bytes it is given may
     stand at another address the next time, because the caller hands it the
     contents of a buffer it grows. */
  SSL_set_mode(ssl, SSL_MODE_ENABLE_PARTIAL_WRITE | SSL_MODE_ACCEPT_MOVING_WRITE_BUFFER);
  return ssl;
}

void zyxtls_conn_free(void *conn) {
  if (conn == NULL) return;
  SSL_free((SSL *)conn);
}

int32_t zyxtls_conn_verify(void *conn, const char *name, size_t len) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  char *host = zyxtls_owned(name, len);
  if (host == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return -1;
  }
  int rc = SSL_set1_host(ssl, host);
  free(host);
  if (rc != 1) {
    zyxtls_fail_openssl(conn, "the name could not be checked against a certificate");
    return -1;
  }
  return 0;
}

int32_t zyxtls_conn_trust_none(void *conn) {
  zyxtls_clear();
  SSL_set_verify((SSL *)conn, SSL_VERIFY_NONE, NULL);
  return 0;
}

int32_t zyxtls_conn_sni(void *conn, const char *name, size_t len) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  char *host = zyxtls_owned(name, len);
  if (host == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return -1;
  }
  int rc = SSL_set_tlsext_host_name(ssl, host);
  free(host);
  if (rc != 1) {
    zyxtls_fail_openssl(conn, "the name could not be sent to the server");
    return -1;
  }
  return 0;
}

int32_t zyxtls_handshake(void *conn) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  int ret = SSL_do_handshake(ssl);
  if (ret == 1) return 1;
  return zyxtls_answer(ssl, ret);
}

int32_t zyxtls_read(void *conn, void *buf, size_t max) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  if (max == 0) return 0;
  /* The read lands in a scratch buffer and is handed to the language's own
     buffer, because a call of this package may not write through a view the
     language owns. A read is asked for at most one chunk, so the caller loops
     for the rest. */
  if (max > 65536) max = 65536;
  unsigned char *scratch = (unsigned char *)malloc(max);
  if (scratch == NULL) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "out of memory");
    return ZYXTLS_FAILED;
  }
  int ret = SSL_read(ssl, scratch, (int)max);
  if (ret > 0) {
    zyxrt_buf_write(buf, scratch, (size_t)ret);
    free(scratch);
    return ret;
  }
  free(scratch);
  return zyxtls_answer(ssl, ret);
}

int32_t zyxtls_write(void *conn, const unsigned char *bytes, size_t len) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  if (len == 0) return 0;
  int cap = len > 65536 ? 65536 : (int)len;
  int ret = SSL_write(ssl, bytes, cap);
  if (ret > 0) return ret;
  return zyxtls_answer(ssl, ret);
}

void zyxtls_flush(void *conn, void *buf) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  BIO *outgoing = SSL_get_wbio(ssl);
  if (outgoing == NULL) return;
  unsigned char chunk[16384];
  for (;;) {
    int n = BIO_read(outgoing, chunk, (int)sizeof(chunk));
    if (n <= 0) return;
    zyxrt_buf_write(buf, chunk, (size_t)n);
  }
}

int32_t zyxtls_feed(void *conn, const unsigned char *bytes, size_t len) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  BIO *incoming = SSL_get_rbio(ssl);
  if (incoming == NULL) return 0;
  int n = BIO_write(incoming, bytes, (int)len);
  if (n <= 0) {
    zyxtls_fail(ZYXTLS_OTHER, 0, "the bytes that arrived could not be held");
    return ZYXTLS_FAILED;
  }
  return n;
}

int32_t zyxtls_shutdown(void *conn) {
  zyxtls_clear();
  SSL *ssl = (SSL *)conn;
  int ret = SSL_shutdown(ssl);
  if (ret >= 0) return 1;
  return zyxtls_answer(ssl, ret);
}
