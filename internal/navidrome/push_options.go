package navidrome

// ManualPushOptions are the options of an explicit push — `udl navidrome
// push` and the native app's Push now: rescan when anything changed and
// re-apply Date Added afterwards, with the default scan waits. The CLI's
// --no-scan and --no-dates flags only ever turn steps off.
func ManualPushOptions(allowDeletes int, forceScan bool) MirrorOptions {
	return MirrorOptions{Scan: true, ForceScan: forceScan, Dates: true, AllowDeletes: allowDeletes}
}
