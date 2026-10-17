/* The certificate and the key a server of this program answers with, carried
   as the text of the two files a TLS configuration names. They are a key and
   a self-signed certificate for localhost, they belong to no one, and they
   are material of a test alone. */

#include "pems.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char certificate[] =
    "-----BEGIN CERTIFICATE-----\n"
    "MIIBlTCCATugAwIBAgIUDxNCKan0Q39cyamnOs02Jt7EXQYwCgYIKoZIzj0EAwIw\n"
    "FDESMBAGA1UEAwwJbG9jYWxob3N0MCAXDTI2MTAxNzE1MjEwNFoYDzIxMjYwOTIz\n"
    "MTUyMTA0WjAUMRIwEAYDVQQDDAlsb2NhbGhvc3QwWTATBgcqhkjOPQIBBggqhkjO\n"
    "PQMBBwNCAAQBUvbzEu7qRaZwJ07+pHcp045pa5qzss5A+4lPLqFsWAVmr+4V/NjL\n"
    "S65orJnJOlcqnA+AkqI0o/SnyAqzL/7ho2kwZzAdBgNVHQ4EFgQUfSMRE7uaBovV\n"
    "oxBkitNn1289GNYwHwYDVR0jBBgwFoAUfSMRE7uaBovVoxBkitNn1289GNYwDwYD\n"
    "VR0TAQH/BAUwAwEB/zAUBgNVHREEDTALgglsb2NhbGhvc3QwCgYIKoZIzj0EAwID\n"
    "SAAwRQIgB+j9GMc63r9jo4uGtAzpgIs6cpnBiU16J6fGK8J78pECIQCy2qZ/s9uM\n"
    "64rL+a8ifowJgxFWyR8m4AiTisPEr7uJAw==\n"
    "-----END CERTIFICATE-----\n"
    ;

static const char private_key[] =
    "-----BEGIN EC PRIVATE KEY-----\n"
    "MHcCAQEEIGf5nTIMJn9Gc+6MmrFvXh02+O+AKlgDLFZU8yrNHyjFoAoGCCqGSM49\n"
    "AwEHoUQDQgAEAVL28xLu6kWmcCdO/qR3KdOOaWuas7LOQPuJTy6hbFgFZq/uFfzY\n"
    "y0uuaKyZyTpXKpwPgJKiNKP0p8gKsy/+4Q==\n"
    "-----END EC PRIVATE KEY-----\n"
    ;

/* zyxtest_write_file writes text to the path a call names. The path is a text
   of the language, which carries its length and no zero byte, so it is copied
   into a string the C library takes. */
static int zyxtest_write_file(const char *path, size_t len, const char *text) {
    char *name = (char *)malloc(len + 1);
    if (name == NULL) return -1;
    memcpy(name, path, len);
    name[len] = 0;
    FILE *file = fopen(name, "wb");
    free(name);
    if (file == NULL) return -1;
    size_t n = strlen(text);
    int wrote = fwrite(text, 1, n, file) == n ? 0 : -1;
    if (fclose(file) != 0) wrote = -1;
    return wrote;
}

int zyxtest_write_material(const char *cert_path, size_t cert_len,
                           const char *key_path, size_t key_len) {
    if (zyxtest_write_file(cert_path, cert_len, certificate) != 0) return -1;
    return zyxtest_write_file(key_path, key_len, private_key);
}
