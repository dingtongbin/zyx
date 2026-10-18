/* table answers the address of a byte pattern the C side owns, and byte_at
   answers the byte at an address, so a program can prove the address it computed
   with an offset is the one it meant. */

unsigned char *table(void);
int byte_at(const unsigned char *p);
