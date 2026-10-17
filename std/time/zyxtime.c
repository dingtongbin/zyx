#include "zyxtime.h"

/* The C compiler is asked for strict ISO C, under which a system hides the
   declarations a program reaches through POSIX. The word has to come before
   any system header, so it is written here first. */
#if !defined(_WIN32)
#if !defined(_GNU_SOURCE)
#define _GNU_SOURCE 1
#endif
#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif
#endif

#if defined(_WIN32)

/* GetSystemTimePreciseAsFileTime answers with the resolution of the system
   timer rather than with the resolution of its tick, and is declared from
   Windows 8. */
#if !defined(_WIN32_WINNT) || _WIN32_WINNT < 0x0602
#undef _WIN32_WINNT
#define _WIN32_WINNT 0x0602
#endif

#include <windows.h>

int64_t zyxtime_wall_nanos(void) {
  FILETIME file;
  ULARGE_INTEGER ticks;
  GetSystemTimePreciseAsFileTime(&file);
  ticks.LowPart = file.dwLowDateTime;
  ticks.HighPart = file.dwHighDateTime;
  /* A FILETIME counts intervals of a hundred nanoseconds from 1601-01-01,
     which is 11644473600 seconds before the epoch. */
  return (int64_t)(ticks.QuadPart - 116444736000000000ull) * 100;
}

#else

#include <time.h>

int64_t zyxtime_wall_nanos(void) {
  struct timespec ts;
  if (clock_gettime(CLOCK_REALTIME, &ts) != 0) {
    return 0;
  }
  return (int64_t)ts.tv_sec * 1000000000ll + (int64_t)ts.tv_nsec;
}

#endif

int64_t zyxtime_duration_ns(uint64_t nanos) { return (int64_t)nanos; }

uint64_t zyxtime_duration_of(int64_t nanos) { return (uint64_t)nanos; }
