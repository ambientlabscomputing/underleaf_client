package version

// These variables can be overridden at build time using ldflags
var (
	Version   = "0.0.0"
	GitCommit = ""
	BuildDate = ""
)
