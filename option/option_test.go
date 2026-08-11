package option

import "testing"

func TestDefaultCompileOptionsAndMutators(t *testing.T) {
	opts := DefaultCompileOptions()
	if opts.MaxInlineDepth != DefaultMaxInlineDepth {
		t.Fatalf("MaxInlineDepth = %d, want %d", opts.MaxInlineDepth, DefaultMaxInlineDepth)
	}
	if opts.RecursiveDepth != DefaultRecursiveDepth {
		t.Fatalf("RecursiveDepth = %d, want %d", opts.RecursiveDepth, DefaultRecursiveDepth)
	}
	if opts.EncOnlyOmitNull != false {
		t.Fatalf("EncOnlyOmitNull = %v, want false", opts.EncOnlyOmitNull)
	}
	WithCompileMaxInlineDepth(7)(&opts)
	WithCompileRecursiveDepth(3)(&opts)
	WithCompileEncOnlyOmitNull(true)(&opts)
	if opts.MaxInlineDepth != 7 || opts.RecursiveDepth != 3 || !opts.EncOnlyOmitNull {
		t.Fatalf("mutated options = %+v", opts)
	}
}

func TestDefaultCompileDepthGlobalsAreAssignable(t *testing.T) {
	origInline := DefaultMaxInlineDepth
	origRecursive := DefaultRecursiveDepth
	t.Cleanup(func() {
		DefaultMaxInlineDepth = origInline
		DefaultRecursiveDepth = origRecursive
	})
	DefaultMaxInlineDepth = 4
	DefaultRecursiveDepth = 2
	opts := DefaultCompileOptions()
	if opts.MaxInlineDepth != 4 || opts.RecursiveDepth != 2 {
		t.Fatalf("DefaultCompileOptions() = %+v, want updated globals", opts)
	}
}

func TestWithCompileMaxInlineDepthPanicsForNonPositive(t *testing.T) {
	assertPanicMessage(t, "depth must be > 0", func() {
		opts := DefaultCompileOptions()
		WithCompileMaxInlineDepth(0)(&opts)
	})
}

func TestWithCompileRecursiveDepthPanicsForNegative(t *testing.T) {
	assertPanicMessage(t, "loop must be >= 0", func() {
		opts := DefaultCompileOptions()
		WithCompileRecursiveDepth(-1)(&opts)
	})
}

func assertPanicMessage(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic %q", want)
		}
		if got != want {
			t.Fatalf("panic = %v, want %q", got, want)
		}
	}()
	fn()
}
