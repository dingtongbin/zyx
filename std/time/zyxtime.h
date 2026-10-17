/* The clock of std/time and the bridge between the language's Duration and
   the count of nanoseconds it is. */

#ifndef ZYXTIME_H
#define ZYXTIME_H

#include <stdint.h>

/* zyxtime_wall_nanos answers the wall clock: the number of nanoseconds since
   1970-01-01T00:00:00Z. */
int64_t zyxtime_wall_nanos(void);

/* zyxtime_duration_ns answers the count of nanoseconds a Duration is, and
   zyxtime_duration_of answers the Duration of a count. The language converts
   a Duration neither to nor from an integer, so the two are the only place
   where a duration meets its own count. Both are identities: a Duration is
   exactly the count of nanoseconds and carries nothing else. */
int64_t zyxtime_duration_ns(uint64_t nanos);
uint64_t zyxtime_duration_of(int64_t nanos);

#endif /* ZYXTIME_H */
