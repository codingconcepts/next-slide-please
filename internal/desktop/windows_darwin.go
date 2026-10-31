package desktop

import (
	"fmt"
	"strings"
)

// How much of the window list to ask for, with kCGNullWindowID for "not
// relative to any particular window". Either list comes back ordered front
// to back, which is the only reason the front application can be found
// without asking AppKit.
//
// On screen means on the Space being looked at right now, so it answers
// "what is the presenter looking at" exactly and answers "where is Keynote"
// badly: a full screen slideshow is on a Space of its own, and the moment
// the presenter switches away from it to anything else it is no longer on
// screen. So the two questions ask for different lists.
const (
	onScreenOnly = 1
	everyWindow  = 0
	nullWindowID = 0
)

// normalLayer is the window layer ordinary application windows live on.
// Menu bar extras, the dock, notifications and the wallpaper all sit on
// layers of their own, and every one of them would otherwise be reported as
// the front application the moment it drew anything.
const normalLayer = 0

// Frontmost is the application the presenter is looking at. Reading the
// window list needs no permission of any kind as long as window *names* are
// not asked for, which is why nsp asks only for the owner and the layer.
func Frontmost() (App, error) {
	var found App

	err := eachWindow(onScreenOnly, func(app App, layer int32) bool {
		if layer != normalLayer || app.PID == 0 {
			return true
		}

		found = app

		return false
	})
	if err != nil {
		return App{}, err
	}

	if found.PID == 0 {
		return App{}, fmt.Errorf("no application has a window on screen")
	}

	return found, nil
}

// Find is the application whose name matches, for a presenter who has named
// their slide application rather than letting nsp work it out. The match is
// case insensitive and on a prefix, so "keynote" finds Keynote and "chrome"
// finds Google Chrome.
//
// Front to back order carries through, so when a name matches more than one
// application - two browsers, say - the one in front wins.
func Find(name string) (App, error) {
	if strings.TrimSpace(name) == "" {
		return App{}, fmt.Errorf("no application name given")
	}

	want := strings.ToLower(strings.TrimSpace(name))

	var found App

	err := eachWindow(everyWindow, func(app App, layer int32) bool {
		if layer != normalLayer || app.PID == 0 {
			return true
		}

		if !strings.Contains(strings.ToLower(app.Name), want) {
			return true
		}

		found = app

		return false
	})
	if err != nil {
		return App{}, err
	}

	if found.PID == 0 {
		return App{}, fmt.Errorf("no running application is called %q", name)
	}

	return found, nil
}

// eachWindow walks windows front to back until fn says to stop.
func eachWindow(option uint32, fn func(app App, layer int32) bool) error {
	if err := load(); err != nil {
		return err
	}

	list := cgWindowListCopyWindowInfo(option, nullWindowID)
	if list == 0 {
		return fmt.Errorf("reading the window list")
	}
	defer cfRelease(list)

	for i, n := 0, cfArrayGetCount(list); i < n; i++ {
		window := cfArrayGetValueAtIndex(list, i)
		if window == 0 {
			continue
		}

		layer, ok := int32Value(cfDictionaryGetValue(window, windowLayer), numberInt)
		if !ok {
			continue
		}

		pid, ok := int32Value(cfDictionaryGetValue(window, windowOwnerPID), numberSInt32)
		if !ok {
			continue
		}

		app := App{
			PID:  pid,
			Name: goString(cfDictionaryGetValue(window, windowOwnerName)),
		}

		if !fn(app, layer) {
			return nil
		}
	}

	return nil
}
