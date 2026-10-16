/* A header beside the program, so the C boundary is read from a file the
   program owns as well as from the runtime's own header. */

enum Level { Low = 1, High = 2 };

extern int counter;

int add(int a, int b);

void trace(const char *fmt, ...);

struct Point { int x; int y; };

int sum(struct Point p);
