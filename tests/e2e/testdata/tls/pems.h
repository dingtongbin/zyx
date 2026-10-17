/* The certificate and the key a server of a test program answers with. */

#ifndef ZYXTEST_PEMS_H
#define ZYXTEST_PEMS_H

#include <stddef.h>

/* zyxtest_write_material writes the certificate and the key to the two paths
   a call names, answering zero or a fault. The program keeps its material in
   a temporary directory of its own, so nothing it writes outlives it. */
int zyxtest_write_material(const char *cert_path, size_t cert_len,
                           const char *key_path, size_t key_len);

#endif /* ZYXTEST_PEMS_H */
