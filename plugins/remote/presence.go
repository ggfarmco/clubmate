package main

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>

static double cm_input_idle(void) {
	CGEventType types[] = {
		kCGEventKeyDown, kCGEventFlagsChanged, kCGEventMouseMoved, kCGEventLeftMouseDown,
		kCGEventRightMouseDown, kCGEventOtherMouseDown, kCGEventLeftMouseDragged, kCGEventScrollWheel,
	};
	double idle = -1;
	for (size_t i = 0; i < sizeof(types) / sizeof(types[0]); i++) {
		double s = CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateHIDSystemState, types[i]);
		if (idle < 0 || s < idle) {
			idle = s;
		}
	}
	return idle;
}

static int cm_flag(CFDictionaryRef d, CFStringRef key) {
	CFTypeRef v = CFDictionaryGetValue(d, key);
	if (!v) {
		return -1;
	}
	if (CFGetTypeID(v) == CFBooleanGetTypeID()) {
		return CFBooleanGetValue((CFBooleanRef)v) ? 1 : 0;
	}
	int n = 0;
	if (CFGetTypeID(v) == CFNumberGetTypeID() && CFNumberGetValue((CFNumberRef)v, kCFNumberIntType, &n)) {
		return n != 0;
	}
	return -1;
}

static int cm_session_away(void) {
	CFDictionaryRef d = CGSessionCopyCurrentDictionary();
	if (!d) {
		return 1;
	}
	int away = cm_flag(d, CFSTR("CGSSessionScreenIsLocked")) == 1 || cm_flag(d, kCGSessionOnConsoleKey) == 0;
	CFRelease(d);
	return away;
}
*/
import "C"

import "time"

const (
	awayAfterEnv     = "CLUBMATE_REMOTE_AWAY_AFTER"
	presenceEnv      = "CLUBMATE_REMOTE_PRESENCE"
	presenceHere     = "here"
	defaultAwayAfter = 3 * time.Minute
)

type presence interface {
	atMac() bool
}

type fixedPresence bool

func (p fixedPresence) atMac() bool {
	return bool(p)
}

type macPresence struct {
	awayAfter time.Duration
}

func (p macPresence) atMac() bool {
	if C.cm_session_away() != 0 {
		return false
	}
	idle := float64(C.cm_input_idle())
	return idle >= 0 && time.Duration(idle*float64(time.Second)) < p.awayAfter
}

func newPresence(env func(string) string) presence {
	if env(testModeEnv) == "1" {
		return fixedPresence(env(presenceEnv) == presenceHere)
	}
	after, err := time.ParseDuration(env(awayAfterEnv))
	if err != nil || after <= 0 {
		after = defaultAwayAfter
	}
	return macPresence{awayAfter: after}
}
