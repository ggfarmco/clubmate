package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdlib.h>
#include <notify.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>

static IOReturn cm_assert(const char *type, const char *name, IOPMAssertionID *id) {
	CFStringRef t = CFStringCreateWithCString(NULL, type, kCFStringEncodingUTF8);
	CFStringRef n = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
	IOReturn r = IOPMAssertionCreateWithName(t, kIOPMAssertionLevelOn, n, id);
	CFRelease(t);
	CFRelease(n);
	return r;
}

static kern_return_t cm_clamshell(uint32_t selector, uint64_t disable) {
	io_service_t root = IOServiceGetMatchingService(kIOMainPortDefault, IOServiceMatching("IOPMrootDomain"));
	if (!root) {
		return kIOReturnNotFound;
	}
	io_connect_t conn;
	kern_return_t r = IOServiceOpen(root, mach_task_self(), 0, &conn);
	IOObjectRelease(root);
	if (r != KERN_SUCCESS) {
		return r;
	}
	r = IOConnectCallScalarMethod(conn, selector, &disable, 1, NULL, NULL);
	IOServiceClose(conn);
	return r;
}

static int cm_power_watch(void) {
	int fd = -1;
	int token;
	if (notify_register_file_descriptor(kIOPSNotifyPowerSource, &fd, 0, &token) != NOTIFY_STATUS_OK) {
		return -1;
	}
	return fd;
}

static int cm_number(CFDictionaryRef d, const char *key) {
	CFStringRef k = CFStringCreateWithCString(NULL, key, kCFStringEncodingUTF8);
	CFNumberRef n = CFDictionaryGetValue(d, k);
	CFRelease(k);
	int v = -1;
	if (n == NULL || !CFNumberGetValue(n, kCFNumberIntType, &v)) {
		return -1;
	}
	return v;
}

static void cm_power(int *laptop, int *battery, int *percent) {
	*laptop = 0;
	*battery = 0;
	*percent = -1;
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (info == NULL) {
		return;
	}
	CFArrayRef list = IOPSCopyPowerSourcesList(info);
	if (list != NULL) {
		for (CFIndex i = 0; i < CFArrayGetCount(list); i++) {
			CFDictionaryRef d = IOPSGetPowerSourceDescription(info, CFArrayGetValueAtIndex(list, i));
			if (d == NULL) {
				continue;
			}
			CFStringRef type = CFDictionaryGetValue(d, CFSTR(kIOPSTypeKey));
			if (type != NULL && CFEqual(type, CFSTR(kIOPSInternalBatteryType))) {
				*laptop = 1;
				int cur = cm_number(d, kIOPSCurrentCapacityKey);
				int max = cm_number(d, kIOPSMaxCapacityKey);
				if (cur >= 0 && max > 0) {
					*percent = cur * 100 / max;
				}
			}
		}
		CFRelease(list);
	}
	CFStringRef source = IOPSGetProvidingPowerSourceType(info);
	if (source != NULL && CFEqual(source, CFSTR(kIOPMBatteryPowerKey))) {
		*battery = 1;
	}
	CFRelease(info);
}
*/
import "C"

import (
	"fmt"
	"os"
	"slices"
	"syscall"
	"unsafe"
)

const (
	assertSystem  = "PreventSystemSleep"
	assertIdle    = "PreventUserIdleSystemSleep"
	assertDisplay = "PreventUserIdleDisplaySleep"
	assertName    = "Clubmate keep awake"

	clamshellSleepSelector = 12
	lowBatteryPercent      = 10
)

type assertion interface {
	Release()
}

type asserter func(types []string) (assertion, error)

type holder struct {
	assert asserter
	types  []string
	held   assertion
}

func (h *holder) want(types []string) error {
	if types == nil {
		h.release()
		return nil
	}
	if h.held != nil && slices.Equal(types, h.types) {
		return nil
	}
	h.release()
	a, err := h.assert(types)
	if err != nil {
		return err
	}
	h.held, h.types = a, types
	return nil
}

func (h *holder) release() {
	if h.held != nil {
		h.held.Release()
	}
	h.held, h.types = nil, nil
}

func (h *holder) on() bool {
	return h.held != nil
}

func assertionTypes(display bool) []string {
	types := []string{assertSystem, assertIdle}
	if display {
		types = append(types, assertDisplay)
	}
	return types
}

type iokitAssertions []C.IOPMAssertionID

func assertIOKit(types []string) (assertion, error) {
	name := C.CString(assertName)
	defer C.free(unsafe.Pointer(name))
	ids := make(iokitAssertions, 0, len(types))
	for _, t := range types {
		ct := C.CString(t)
		var id C.IOPMAssertionID
		r := C.cm_assert(ct, name, &id)
		C.free(unsafe.Pointer(ct))
		if r != C.kIOReturnSuccess {
			ids.Release()
			return nil, fmt.Errorf("IOPMAssertionCreateWithName %s: 0x%x", t, uint32(r))
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (ids iokitAssertions) Release() {
	for _, id := range ids {
		C.IOPMAssertionRelease(id)
	}
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func readPower() power {
	var laptop, battery, percent C.int
	C.cm_power(&laptop, &battery, &percent)
	onBattery := battery != 0
	return power{Laptop: laptop != 0, OnBattery: onBattery, Low: onBattery && percent >= 0 && percent < lowBatteryPercent}
}

func setClamshellIOKit(disable bool) error {
	var in C.uint64_t
	if disable {
		in = 1
	}
	if r := C.cm_clamshell(clamshellSleepSelector, in); r != C.KERN_SUCCESS {
		return fmt.Errorf("IOPMrootDomain clamshell sleep: 0x%x", uint32(r))
	}
	return nil
}

func watchPower(changed func()) {
	fd := C.cm_power_watch()
	if fd < 0 {
		return
	}
	f := os.NewFile(uintptr(fd), "powersource")
	defer f.Close()
	buf := make([]byte, 4)
	for {
		if _, err := f.Read(buf); err != nil {
			return
		}
		changed()
	}
}
