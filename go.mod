module github.com/cacack/gedcom-cli

go 1.25.0

// The toolchain CI and releases build with (setup-go reads it).
toolchain go1.27.4

require (
	github.com/cacack/gedcom-go/v3 v3.15.0
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/text v0.41.0 // indirect
)
