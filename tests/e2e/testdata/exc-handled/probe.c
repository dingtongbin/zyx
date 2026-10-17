#include "probe.h"

#include <windows.h>

/* A library that raises an exception of its own and takes it back. Its handler is
   added behind the ones already installed, which is where a library's handler
   belongs: the exceptions that are already recognized as faults of the program are
   not its business, and the ones it raised itself are. It answers the two codes it
   raises and lets anything else pass, and it counts what it took, so the program
   can tell whether the exceptions reached it at all. */

static int seen;

static LONG CALLBACK take_own(PEXCEPTION_POINTERS info) {
  DWORD code = info->ExceptionRecord->ExceptionCode;
  if (code != 0xE06D7363u && code != 0x406D1388u) {
    return EXCEPTION_CONTINUE_SEARCH;
  }
  seen++;
  return EXCEPTION_CONTINUE_EXECUTION;
}

int probe_caught(void) {
  AddVectoredExceptionHandler(0, take_own);
  RaiseException(0xE06D7363u, 0, 0, NULL);
  RaiseException(0x406D1388u, 0, 0, NULL);
  return seen;
}
