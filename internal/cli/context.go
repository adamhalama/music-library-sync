package cli

import "io"

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

type GlobalOptions struct {
	ConfigPath    string
	JSON          bool
	Quiet         bool
	Verbose       bool
	NoColor       bool
	NoInput       bool
	DryRun        bool
	AskOnExisting bool
	ScanGaps      bool
	NoPreflight   bool
}

type AppContext struct {
	Build BuildInfo
	IO    IOStreams
	Opts  GlobalOptions
}
