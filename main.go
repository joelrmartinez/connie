// Command connie scrapes public ATS job boards (Ashby, Greenhouse, Lever),
// remembers every posting it has ever seen, and tells you what's new.
//
// Named for Connie Sachs, the Circus's memory.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Wired to internal/cli in Phase 8; keep the binary compiling until then.
	fmt.Fprintln(os.Stderr, "connie: not yet operational — see https://github.com/saiwrd/connie")
	os.Exit(2)
}
