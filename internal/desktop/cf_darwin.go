package desktop

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const supported = true

// Both frameworks are part of every macOS install, so they are loaded by path
// at first use rather than linked. This is what keeps the build free of cgo:
// purego resolves the symbols at runtime.
const (
	coreFoundation = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	coreGraphics   = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
)

// kCFStringEncodingUTF8, which is the only encoding anything here asks for.
const encodingUTF8 = 0x08000100

// CFNumber types, as used to read a window's owner and layer back out of the
// dictionaries CoreGraphics hands over.
const (
	numberSInt32 = 3
	numberInt    = 9
)

var (
	loadOnce sync.Once
	loadErr  error

	cfArrayGetCount           func(array uintptr) int
	cfArrayGetValueAtIndex    func(array uintptr, index int) uintptr
	cfDictionaryGetValue      func(dict, key uintptr) uintptr
	cfStringCreateWithCString func(allocator uintptr, str string, encoding uint32) uintptr
	cfStringGetCString        func(str uintptr, buf unsafe.Pointer, size int, encoding uint32) bool
	cfNumberGetValue          func(number uintptr, numberType int, value unsafe.Pointer) bool
	cfRelease                 func(ref uintptr)

	cfMachPortCreateRunLoopSource func(allocator, port uintptr, order int) uintptr
	cfRunLoopGetCurrent           func() uintptr
	cfRunLoopAddSource            func(runLoop, source, mode uintptr)
	cfRunLoopRun                  func()
	cfRunLoopStop                 func(runLoop uintptr)

	cgWindowListCopyWindowInfo  func(option uint32, relativeTo uint32) uintptr
	cgEventTapCreate            func(tap, place, options uint32, eventsOfInterest uint64, callback, userInfo uintptr) uintptr
	cgEventTapEnable            func(port uintptr, enable bool)
	cgEventGetIntegerValueField func(event uintptr, field uint32) int64
	cgEventGetFlags             func(event uintptr) uint64

	// Framework constants. Each of these is declared as a CFStringRef whose
	// contents are its own name, and each is looked up by string equality -
	// a run loop mode in a set, a window property in a dictionary - so
	// rather than reach into the frameworks' data segments for the
	// originals, nsp builds strings that compare equal to them. Held for the
	// life of the process, because that's  how long they are constants for.
	//
	// The default run loop mode rather than the common modes, which is the
	// one constant this trick does not work for: CoreFoundation recognises
	// the common modes by pointer rather than by contents, and a copy that
	// merely reads the same is quietly accepted and never run. Nothing is
	// lost by it, because the run loop the event tap is attached to is one
	// nsp started for the tap alone and runs in no other mode.
	runLoopDefaultMode uintptr
	windowOwnerName    uintptr
	windowOwnerPID     uintptr
	windowLayer        uintptr
)

func load() error {
	loadOnce.Do(func() {
		cf, err := purego.Dlopen(coreFoundation, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("loading CoreFoundation: %w", err)
			return
		}

		cg, err := purego.Dlopen(coreGraphics, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("loading CoreGraphics: %w", err)
			return
		}

		purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")
		purego.RegisterLibFunc(&cfDictionaryGetValue, cf, "CFDictionaryGetValue")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")

		purego.RegisterLibFunc(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
		purego.RegisterLibFunc(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfRunLoopRun, cf, "CFRunLoopRun")
		purego.RegisterLibFunc(&cfRunLoopStop, cf, "CFRunLoopStop")

		purego.RegisterLibFunc(&cgWindowListCopyWindowInfo, cg, "CGWindowListCopyWindowInfo")
		purego.RegisterLibFunc(&cgEventTapCreate, cg, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgEventTapEnable, cg, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")
		purego.RegisterLibFunc(&cgEventGetFlags, cg, "CGEventGetFlags")

		for _, c := range []struct {
			into *uintptr
			name string
		}{
			{&runLoopDefaultMode, "kCFRunLoopDefaultMode"},
			{&windowOwnerName, "kCGWindowOwnerName"},
			{&windowOwnerPID, "kCGWindowOwnerPID"},
			{&windowLayer, "kCGWindowLayer"},
		} {
			if *c.into = cfStringCreateWithCString(0, c.name, encodingUTF8); *c.into == 0 {
				loadErr = fmt.Errorf("building the constant %s", c.name)
				return
			}
		}
	})

	return loadErr
}

// goString copies a CFStringRef out into Go memory. An application name that
// does not fit is not one nsp needs to know about.
func goString(ref uintptr) string {
	if ref == 0 {
		return ""
	}

	buf := make([]byte, 256)
	if !cfStringGetCString(ref, unsafe.Pointer(&buf[0]), len(buf), encodingUTF8) {
		return ""
	}

	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}

	return string(buf)
}

// int32Value reads a CFNumberRef. CFNumberGetValue converts between types
// rather than refusing, so the type asked for is the type that comes back.
func int32Value(ref uintptr, numberType int) (int32, bool) {
	if ref == 0 {
		return 0, false
	}

	var out int32
	if !cfNumberGetValue(ref, numberType, unsafe.Pointer(&out)) {
		return 0, false
	}

	return out, true
}
