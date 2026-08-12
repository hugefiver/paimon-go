//go:build !sonic_stdjson

package compatmode

// StdJSON reports whether the build prefers strict standard-library JSON
// behavior over Sonic-compatible raw parser behavior.
const StdJSON = false

// SonicCompat reports whether known non-standard Sonic parser behavior is
// enabled for raw JSON entry points.
const SonicCompat = true
