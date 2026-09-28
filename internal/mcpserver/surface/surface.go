// Package surface holds the compiled-in MCP tool and prompt descriptions.
//
// They are embedded at build time and never derived from index or network
// data (docs/security.md §2.2): a poisoned shard cannot change what the model
// is told about the tools. Golden tests pin the resulting tools/list bytes.
package surface

import (
	_ "embed"
	"strings"
)

var (
	//go:embed instructions.md
	instructions string
	//go:embed frc_search.md
	search string
	//go:embed frc_fetch.md
	fetch string
	//go:embed frc_api.md
	api string
	//go:embed frc_context.md
	context string
)

// Instructions is the server-level instructions text.
func Instructions() string { return strings.TrimSpace(instructions) }

// Search, Fetch and API return tool descriptions.
func Search() string  { return strings.TrimSpace(search) }
func Fetch() string   { return strings.TrimSpace(fetch) }
func API() string     { return strings.TrimSpace(api) }
func Context() string { return strings.TrimSpace(context) }
