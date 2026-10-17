/* Descriptors for the event loop's test. A pair of connected stream sockets,
   one end of which can be waited on, and a pair whose first end has a number
   the caller asks for. */

#ifndef LOOPFD_H
#define LOOPFD_H

/* loopfd_pair makes a pair of connected stream sockets, both non-blocking, and
   answers 0 or -1. */
int loopfd_pair(int *a, int *b);

/* loopfd_pair_number makes connected pairs until one end of one has the number
   wanted, and answers that pair with the end that has it first. It answers -1
   when the system does not hand the number back. */
int loopfd_pair_number(int wanted, int *a, int *b);

/* loopfd_write sends one byte. */
int loopfd_write(int fd, int byte);

/* loopfd_close closes the descriptor. */
int loopfd_close(int fd);

#endif
