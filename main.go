// Command connie scrapes public ATS job boards (Ashby,
// Greenhouse, Lever, Workable), remembers every posting it has ever seen,
// and tells you what's new.
//
// Named for Connie Sachs, the Circus's memory.
package main

import (
	"os"

	"github.com/joelrmartinez/connie/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
