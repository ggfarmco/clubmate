package proc

/*
#include <mach/mach.h>
#include <mach/mach_host.h>

static int cm_host_cpu(unsigned long long *busy, unsigned long long *total) {
	host_cpu_load_info_data_t info;
	mach_msg_type_number_t count = HOST_CPU_LOAD_INFO_COUNT;
	mach_port_t host = mach_host_self();
	kern_return_t kr = host_statistics(host, HOST_CPU_LOAD_INFO, (host_info_t)&info, &count);
	mach_port_deallocate(mach_task_self(), host);
	if (kr != KERN_SUCCESS) {
		return 0;
	}
	unsigned long long used = (unsigned long long)info.cpu_ticks[CPU_STATE_USER] + info.cpu_ticks[CPU_STATE_SYSTEM] + info.cpu_ticks[CPU_STATE_NICE];
	*busy = used;
	*total = used + info.cpu_ticks[CPU_STATE_IDLE];
	return 1;
}

static int cm_host_mem_free(unsigned long long *free) {
	vm_statistics64_data_t vm;
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	mach_port_t host = mach_host_self();
	vm_size_t page = 0;
	kern_return_t kr = host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &count);
	kern_return_t pr = host_page_size(host, &page);
	mach_port_deallocate(mach_task_self(), host);
	if (kr != KERN_SUCCESS || pr != KERN_SUCCESS) {
		return 0;
	}
	*free = ((unsigned long long)vm.free_count + vm.inactive_count) * page;
	return 1;
}
*/
import "C"

import "errors"

var errHostStats = errors.New("host statistics unavailable")

type HostTicks struct {
	Busy  uint64
	Total uint64
}

func HostCPUTicks() (HostTicks, error) {
	var busy, total C.ulonglong
	if C.cm_host_cpu(&busy, &total) == 0 {
		return HostTicks{}, errHostStats
	}
	return HostTicks{Busy: uint64(busy), Total: uint64(total)}, nil
}

func HostMemFree() (uint64, error) {
	var free C.ulonglong
	if C.cm_host_mem_free(&free) == 0 {
		return 0, errHostStats
	}
	return uint64(free), nil
}
