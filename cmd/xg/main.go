// Command xg is the terminal radar for build-events: hackathons, buildathons,
// fellowships and other events you compete in by building something.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ssd-81/x-games/internal/cli"
)

func main() {
	code := cli.Run(os.Args[1:])
	os.Exit(code)
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
