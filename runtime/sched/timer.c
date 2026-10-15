/* The monotonic clock and the scheduler's sleep. A deadline is an absolute
   count of nanoseconds, so arithmetic on it is plain unsigned arithmetic. */

#include "zyxrt_internal.h"

#if defined(_WIN32)

#include <windows.h>

static double ticks_per_ns = 0.0;

void zyxrt_timer_init(void) {
  LARGE_INTEGER freq;
  if (QueryPerformanceFrequency(&freq) && freq.QuadPart > 0) {
    ticks_per_ns = (double)freq.QuadPart / 1000000000.0;
  }
}

uint64_t zyxrt_now(void) {
  LARGE_INTEGER counter;
  if (!QueryPerformanceCounter(&counter) || ticks_per_ns == 0.0) {
    return 0;
  }
  return (uint64_t)((double)counter.QuadPart / ticks_per_ns);
}

void zyxrt_sleep(uint64_t ns) {
  /* A millisecond is the finest wait the platform offers. */
  DWORD ms = (DWORD)(ns / 1000000);
  Sleep(ms == 0 ? 1 : ms);
}

#else

#include <time.h>

void zyxrt_timer_init(void) {}

uint64_t zyxrt_now(void) {
  struct timespec ts;
  if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
    return 0;
  }
  return (uint64_t)ts.tv_sec * 1000000000u + (uint64_t)ts.tv_nsec;
}

void zyxrt_sleep(uint64_t ns) {
  struct timespec ts;
  ts.tv_sec = (time_t)(ns / 1000000000u);
  ts.tv_nsec = (long)(ns % 1000000000u);
  nanosleep(&ts, NULL);
}

#endif
