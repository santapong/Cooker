package main

import "os"

// main is intentionally thin: all logic lives in run(), which returns an
// exit code and takes injectable writers so it is unit-testable without a
// subprocess.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
