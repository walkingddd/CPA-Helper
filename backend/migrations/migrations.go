package migrations

import "embed"

// LatestVersion is the newest embedded migration version this binary expects.
const LatestVersion int64 = 202607020001

// FS contains SQL migrations embedded into the application binary.
//
//go:embed *.sql
var FS embed.FS
