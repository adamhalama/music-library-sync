package navidrome

import "testing"

// The CLI's `udl navidrome push` and the app's Push now share these options;
// an explicit push always rescans changes and re-applies Date Added.
func TestManualPushOptionsScanAndReapplyDates(t *testing.T) {
	opts := ManualPushOptions(300, true)
	if !opts.Scan || !opts.Dates || !opts.ForceScan || opts.AllowDeletes != 300 {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.IdleWait != 0 || opts.FinishWait != 0 {
		t.Fatalf("an explicit push uses the default scan waits, not the post-sync ones: %+v", opts)
	}
	if plain := ManualPushOptions(0, false); plain.ForceScan || plain.AllowDeletes != 0 {
		t.Fatalf("plain = %+v", plain)
	}
}
