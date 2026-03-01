package migrations

import "embed"

// FS contains all SQL migration files embedded at compile time.
//
//go:embed *.sql
var FS embed.FS
