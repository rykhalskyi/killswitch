// Package version holds the application version.
package version

// Version is the Kill Switch application version.
const Version = "0.1.5"

// Display returns the version prefixed with "v".
func Display() string {
	return "v" + Version
}
