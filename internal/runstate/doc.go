// Package runstate derives frontend-neutral sync state from plans and runtime
// events.
//
// Track events are matched to plan rows in stable priority order: remote track
// ID, exact title, normalized title, then execution slot. Runtime transitions
// are monotonic for terminal events: done, skip, and fail replace queued or
// downloading state and preserve the terminal outcome in subsequent snapshots.
package runstate
