// Package version menyimpan identitas build lamund.
package version

// Version di-override saat rilis via -ldflags "-X ...version.Version=v0.1.0".
var Version = "v0.0.1-dev"

func String() string { return "lamund " + Version }
