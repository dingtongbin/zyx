/* A pair of connected descriptors, so a program can watch one end while it
   writes to the other. */

int pair_open(int *a, int *b);

int pair_write(int fd, int byte);

int pair_read(int fd);

int pair_close(int fd);
