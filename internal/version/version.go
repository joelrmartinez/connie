// Package version holds connie's single version string.
//
// Release builds override it at link time:
//
//	go build -ldflags "-X github.com/joelrmartinez/connie/internal/version.Version=1.2.3"
package version

// Version is the connie release version. "dev" means an unstamped build.
var Version = "0.1.0"

// UserAgent returns the default User-Agent, with an optional contact address.
func UserAgent(contact string) string {
	if contact == "" {
		return "connie/" + Version + " (+https://github.com/joelrmartinez/connie)"
	}
	return "connie/" + Version + " (+https://github.com/joelrmartinez/connie; contact: " + contact + ")"
}
