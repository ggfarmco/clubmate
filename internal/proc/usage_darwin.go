package proc

/*
#include <libproc.h>
#include <mach/mach_time.h>

static int cm_task_usage(int pid, unsigned long long *rss, unsigned long long *ticks) {
	struct proc_taskinfo ti;
	if (proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &ti, sizeof ti) != sizeof ti) {
		return 0;
	}
	*rss = ti.pti_resident_size;
	*ticks = ti.pti_total_user + ti.pti_total_system;
	return 1;
}

static void cm_timebase(unsigned int *numer, unsigned int *denom) {
	mach_timebase_info_data_t tb;
	mach_timebase_info(&tb);
	*numer = tb.numer;
	*denom = tb.denom;
}
*/
import "C"

import "sync"

var timebase = sync.OnceValues(func() (uint64, uint64) {
	var numer, denom C.uint
	C.cm_timebase(&numer, &denom)
	if denom == 0 {
		return 1, 1
	}
	return uint64(numer), uint64(denom)
})

func taskUsage(pid int) (rssBytes, cpuNs uint64, ok bool) {
	var rss, ticks C.ulonglong
	if C.cm_task_usage(C.int(pid), &rss, &ticks) == 0 {
		return 0, 0, false
	}
	numer, denom := timebase()
	return uint64(rss), uint64(ticks) * numer / denom, true
}
