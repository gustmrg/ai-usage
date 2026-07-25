package aiusage

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var sourceVersion string

var BuiltVersion string

func Version() string {
	if value := strings.TrimSpace(BuiltVersion); value != "" {
		return strings.TrimPrefix(value, "v")
	}
	return strings.TrimSpace(sourceVersion)
}
