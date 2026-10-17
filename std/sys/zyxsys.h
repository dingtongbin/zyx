/* The system information of std/sys. The package answers what the machine is:
   how many processors it has, how much memory it holds, how long it has been
   up, which system it runs, and its load. Each call reaches the operating
   system through the interface that system provides, and the two families of
   targets are written where they differ. */

#ifndef ZYXSYS_H
#define ZYXSYS_H

#include <stddef.h>
#include <stdint.h>

/* zyxsys_cpu_count answers the number of processors the program may run on. */
int32_t zyxsys_cpu_count(void);

/* zyxsys_mem_total answers the physical memory of the machine in bytes, and
   zyxsys_mem_available what the system believes a program could still have
   without swapping. */
uint64_t zyxsys_mem_total(void);
uint64_t zyxsys_mem_available(void);

/* zyxsys_uptime_ns answers how long the machine has been up, in nanoseconds. */
uint64_t zyxsys_uptime_ns(void);

/* zyxsys_os_version and zyxsys_kernel_version append the text each names to a
   buffer, which the caller hands over as its handle. */
void zyxsys_os_version(void *buf);
void zyxsys_kernel_version(void *buf);

/* zyxsys_load_avg writes the one-, five-, and fifteen-minute run-queue
   averages, which a system that keeps none leaves at zero. */
void zyxsys_load_avg(double *one, double *five, double *fifteen);

#endif /* ZYXSYS_H */
