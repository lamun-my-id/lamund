package main

import (
	"fmt"
	"os"

	"github.com/lamun-my-id/lamund/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: lamund <version|serve|site|user|api|backup|dns> ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version.String())
	case "backup":
		cmdBackup(os.Args[2:])
	case "dns":
		cmdDNS(os.Args[2:])
	case "site":
		cmdSite(os.Args[2:])
	case "user":
		cmdUser(os.Args[2:])
	case "api":
		cmdAPI(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "reload":
		cmdReload(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %s\n", os.Args[1])
		os.Exit(2)
	}
}
