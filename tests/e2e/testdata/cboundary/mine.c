#include "mine.h"

int counter = 40;

int add(int a, int b) { return a + b; }

void trace(const char *fmt, ...) { (void)fmt; }

int sum(struct Point p) { return p.x + p.y; }
