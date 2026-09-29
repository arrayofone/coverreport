// Command coverreport computes, checks and ratchets coverage for a repo whose
// layers produce Go coverprofiles and LCOV tracefiles. See README.md.
//
//	go run github.com/arrayofone/coverreport/cmd/coverreport@<sha> check
package main

import (
	"os"

	"github.com/arrayofone/coverreport/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.OSEnv()))
}
