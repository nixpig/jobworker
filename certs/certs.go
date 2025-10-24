// Package certs embeds the pre-generated certificates for use in tests.
package certs

import "embed"

//go:embed *.crt *.key
var FS embed.FS
