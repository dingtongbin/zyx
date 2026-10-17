#include "zyxsys.h"

#include "zyxrt.h"

/* The C compiler is asked for strict ISO C, under which a system hides the
   declarations this file is written against. The word has to come before any
   system header, so it is written here first. */
#if !defined(_WIN32)
#if !defined(_GNU_SOURCE)
#define _GNU_SOURCE 1
#endif
#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif
#endif

#include <stdio.h>
#include <string.h>

/* zyxsys_write appends a terminated text to a buffer the language owns. */
static void zyxsys_write(void *buf, const char *text) {
  zyxrt_buf_write(buf, (const unsigned char *)text, strlen(text));
}

#if defined(_WIN32)

/* GetTickCount64 and GetActiveProcessorCount are declared from the versions
   the two appeared in. */
#if !defined(_WIN32_WINNT) || _WIN32_WINNT < 0x0602
#undef _WIN32_WINNT
#define _WIN32_WINNT 0x0602
#endif

#include <windows.h>

int32_t zyxsys_cpu_count(void) {
  DWORD count = GetActiveProcessorCount(ALL_PROCESSOR_GROUPS);
  if (count == 0) {
    SYSTEM_INFO info;
    GetSystemInfo(&info);
    count = info.dwNumberOfProcessors;
  }
  return (int32_t)count;
}

uint64_t zyxsys_mem_total(void) {
  MEMORYSTATUSEX status;
  memset(&status, 0, sizeof(status));
  status.dwLength = sizeof(status);
  if (!GlobalMemoryStatusEx(&status)) return 0;
  return (uint64_t)status.ullTotalPhys;
}

uint64_t zyxsys_mem_available(void) {
  MEMORYSTATUSEX status;
  memset(&status, 0, sizeof(status));
  status.dwLength = sizeof(status);
  if (!GlobalMemoryStatusEx(&status)) return 0;
  return (uint64_t)status.ullAvailPhys;
}

uint64_t zyxsys_uptime_ns(void) {
  /* The tick count is milliseconds since the system started. */
  return (uint64_t)GetTickCount64() * 1000000ull;
}

/* The version of the running system. GetVersionEx lies about the version
   unless the program carries a manifest, so the kernel's own answer is read
   instead, which is what the version in a report means. */
static void zyxsys_nt_version(uint32_t *major, uint32_t *minor, uint32_t *build) {
  typedef struct {
    ULONG size;
    ULONG major;
    ULONG minor;
    ULONG build;
    ULONG platform;
    WCHAR service_pack[128];
  } zyxsys_version_info;
  zyxsys_version_info info;
  memset(&info, 0, sizeof(info));
  info.size = (ULONG)sizeof(info);
  HMODULE ntdll = GetModuleHandleW(L"ntdll.dll");
  if (ntdll != NULL) {
    typedef LONG(WINAPI * zyxsys_rtl_get_version)(void *);
    zyxsys_rtl_get_version call =
        (zyxsys_rtl_get_version)(void *)GetProcAddress(ntdll, "RtlGetVersion");
    if (call != NULL && call(&info) == 0) {
      *major = info.major;
      *minor = info.minor;
      *build = info.build;
      return;
    }
  }
  *major = 0;
  *minor = 0;
  *build = 0;
}

void zyxsys_os_version(void *buf) {
  uint32_t major, minor, build;
  zyxsys_nt_version(&major, &minor, &build);
  char text[64];
  snprintf(text, sizeof(text), "Windows %u.%u.%u", (unsigned)major,
           (unsigned)minor, (unsigned)build);
  zyxsys_write(buf, text);
}

void zyxsys_kernel_version(void *buf) {
  uint32_t major, minor, build;
  zyxsys_nt_version(&major, &minor, &build);
  char text[64];
  snprintf(text, sizeof(text), "NT %u.%u.%u", (unsigned)major, (unsigned)minor,
           (unsigned)build);
  zyxsys_write(buf, text);
}

void zyxsys_load_avg(double *one, double *five, double *fifteen) {
  /* Windows keeps no run-queue average, so every window reads zero. */
  *one = 0.0;
  *five = 0.0;
  *fifteen = 0.0;
}

#else

#include <stdlib.h>
#include <sys/utsname.h>
#include <time.h>
#include <unistd.h>

#if defined(__APPLE__)
#include <sys/sysctl.h>
#endif

int32_t zyxsys_cpu_count(void) {
  long count = sysconf(_SC_NPROCESSORS_ONLN);
  if (count < 1) return 1;
  return (int32_t)count;
}

uint64_t zyxsys_mem_total(void) {
  long pages = sysconf(_SC_PHYS_PAGES);
  long size = sysconf(_SC_PAGESIZE);
  if (pages < 0 || size < 1) return 0;
  return (uint64_t)pages * (uint64_t)size;
}

uint64_t zyxsys_mem_available(void) {
  long pages = sysconf(_SC_PHYS_PAGES);
  long size = sysconf(_SC_PAGESIZE);
  long available = sysconf(_SC_AVPHYS_PAGES);
  if (pages < 0 || size < 1) return 0;
  if (available < 0) return (uint64_t)pages * (uint64_t)size;
  return (uint64_t)available * (uint64_t)size;
}

uint64_t zyxsys_uptime_ns(void) {
#if defined(CLOCK_BOOTTIME)
  /* A boot clock counts from the moment the system started. */
  struct timespec ts;
  if (clock_gettime(CLOCK_BOOTTIME, &ts) == 0) {
    return (uint64_t)ts.tv_sec * 1000000000ull + (uint64_t)ts.tv_nsec;
  }
#elif defined(__APPLE__)
  /* A system without the boot clock answers when the system started instead,
     which the wall clock is compared against. */
  struct timeval boot;
  size_t size = sizeof(boot);
  int name[2] = {CTL_KERN, KERN_BOOTTIME};
  if (sysctl(name, 2, &boot, &size, NULL, 0) == 0) {
    struct timeval now;
    if (gettimeofday(&now, NULL) == 0) {
      uint64_t seconds = (uint64_t)(now.tv_sec - boot.tv_sec);
      int64_t nanos = (int64_t)now.tv_usec - (int64_t)boot.tv_usec;
      return seconds * 1000000000ull + (uint64_t)(nanos * 1000);
    }
  }
#endif
  /* A target with neither answers with the clock it does have. */
  {
    struct timespec ts;
    if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) return 0;
    return (uint64_t)ts.tv_sec * 1000000000ull + (uint64_t)ts.tv_nsec;
  }
}

void zyxsys_os_version(void *buf) {
  struct utsname name;
  if (uname(&name) != 0) return;
  zyxsys_write(buf, name.sysname);
}

void zyxsys_kernel_version(void *buf) {
  struct utsname name;
  if (uname(&name) != 0) return;
  zyxsys_write(buf, name.release);
}

void zyxsys_load_avg(double *one, double *five, double *fifteen) {
  double values[3] = {0.0, 0.0, 0.0};
  int count = getloadavg(values, 3);
  if (count < 0) count = 0;
  if (count > 0) *one = values[0];
  if (count > 1) *five = values[1];
  if (count > 2) *fifteen = values[2];
  if (count < 1) *one = 0.0;
  if (count < 2) *five = 0.0;
  if (count < 3) *fifteen = 0.0;
}

#endif
