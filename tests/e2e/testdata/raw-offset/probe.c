#include "probe.h"

/* The bytes are ten apart from each other, so a wrong offset reads a value that
   is not the one the program prints. */
static unsigned char bytes[16] = {1, 2, 4, 8, 16, 32, 64, 128, 9, 10, 11, 12, 13, 14, 15, 16};

unsigned char *table(void) {
  return bytes;
}

int byte_at(const unsigned char *p) {
  return (int)*p;
}
