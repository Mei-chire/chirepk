package frontend

import "embed"

// Files contains the browser application shipped with the backend binary.
// Keeping the asset boundary here lets the frontend be replaced by a separate
// build pipeline without leaking files into backend packages.
//
//go:embed static
var Files embed.FS
