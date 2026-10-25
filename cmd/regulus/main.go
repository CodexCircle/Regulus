// Command regulus is the single Regulus executable. The role it plays is
// chosen with a subcommand; for now only `hub` is implemented.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/CodexCircle/Regulus/internal/hub"
	"github.com/CodexCircle/Regulus/internal/webui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hub":
		os.Exit(runHub(os.Args[2:]))
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "regulus: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: regulus <command>

Commands:
  hub      run the hub (serves the dashboard)
  version  print the version
`)
}

func runHub(args []string) int {
	fs := flag.NewFlagSet("hub", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "address to listen on")
	_ = fs.Parse(args)

	slog.Info("starting hub", "version", version, "listen", *listen)
	if err := http.ListenAndServe(*listen, hub.NewHandler(webui.FS())); err != nil {
		slog.Error("hub stopped", "err", err)
		return 1
	}
	return 0
}
