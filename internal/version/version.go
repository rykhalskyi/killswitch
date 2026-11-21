// Package version holds the application version.
package version

// Version is the Kill Switch application version.
const Version = "0.3.0"

// Display returns the version prefixed with "v".
func Display() string {
	return "v" + Version
}
