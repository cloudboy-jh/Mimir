package main

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionString() string {
	if commit == "unknown" {
		return version
	}
	return version + " (" + commit + ")"
}
