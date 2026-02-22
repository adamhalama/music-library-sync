package exitcode

const (
	Success           = 0
	RuntimeFailure    = 1
	InvalidUsage      = 2
	InvalidConfig     = 3
	MissingDependency = 4
	PartialSuccess    = 5
	Interrupted       = 130
)
