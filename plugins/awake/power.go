package main

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdlib.h>
#include <IOKit/pwr_mgt/IOPMLib.h>

static IOReturn cm_assert(const char *type, const char *name, IOPMAssertionID *id) {
	CFStringRef t = CFStringCreateWithCString(NULL, type, kCFStringEncodingUTF8);
	CFStringRef n = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
	IOReturn r = IOPMAssertionCreateWithName(t, kIOPMAssertionLevelOn, n, id);
	CFRelease(t);
	CFRelease(n);
	return r;
}
*/
import "C"

import (
	"fmt"
	"slices"
	"syscall"
	"unsafe"
)

const (
	assertSystem  = "PreventSystemSleep"
	assertIdle    = "PreventUserIdleSystemSleep"
	assertDisplay = "PreventUserIdleDisplaySleep"
	assertName    = "Clubmate keep awake"
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

func assertionTypes(battery, display bool) []string {
	types := []string{assertSystem}
	if battery {
		types = append(types, assertIdle)
	}
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
