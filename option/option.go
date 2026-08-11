// Package option mirrors the Sonic option package's compile-time tuning
// surface. The default buffer sizes and compile option mutators keep the
// same names and semantics as upstream Sonic v1.15.2 so callers using the
// Pretouch API can depend on them without code changes.
package option

// Default buffer sizes used by the encoder/decoder streams. They are
// exported variables (not constants) to match Sonic's public surface,
// allowing callers to retune them at process start-up if needed.
var (
	DefaultDecoderBufferSize uint = 4 * 1024
	DefaultEncoderBufferSize uint = 4 * 1024
	DefaultAstBufferSize     uint = 4 * 1024
)

// LimitBufferSize is the upper bound enforced on user-supplied buffer
// sizes. It is an exported variable to match Sonic's surface.
var LimitBufferSize uint = 1024 * 1024

// Default compile tuning values. They are exported mutable variables to match
// Sonic's public source-compatible surface.
var (
	DefaultMaxInlineDepth = 3
	DefaultRecursiveDepth = 1
)

// CompileOptions groups the compile-time tuning knobs exposed by Sonic's
// Pretouch API. Callers do not usually construct this directly; they use
// DefaultCompileOptions together with the WithCompile* mutators.
type CompileOptions struct {
	MaxInlineDepth  int
	RecursiveDepth  int
	EncOnlyOmitNull bool
}

// CompileOption is the functional-option type used by Pretouch and
// related entry points.
type CompileOption func(o *CompileOptions)

// DefaultCompileOptions returns the default compile options that match
// upstream Sonic's built-in defaults.
func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		MaxInlineDepth: DefaultMaxInlineDepth,
		RecursiveDepth: DefaultRecursiveDepth,
	}
}

// WithCompileMaxInlineDepth returns a CompileOption that sets
// MaxInlineDepth.
func WithCompileMaxInlineDepth(depth int) CompileOption {
	return func(o *CompileOptions) {
		if depth <= 0 {
			panic("depth must be > 0")
		}
		o.MaxInlineDepth = depth
	}
}

// WithCompileRecursiveDepth returns a CompileOption that sets
// RecursiveDepth.
func WithCompileRecursiveDepth(loop int) CompileOption {
	return func(o *CompileOptions) {
		if loop < 0 {
			panic("loop must be >= 0")
		}
		o.RecursiveDepth = loop
	}
}

// WithCompileEncOnlyOmitNull returns a CompileOption that toggles
// EncOnlyOmitNull.
func WithCompileEncOnlyOmitNull(omit bool) CompileOption {
	return func(o *CompileOptions) { o.EncOnlyOmitNull = omit }
}
