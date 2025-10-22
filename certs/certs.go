package certs

import "embed"

//go:embed *.crt *.key
var FS embed.FS
