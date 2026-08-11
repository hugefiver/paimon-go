package utf8

import "testing"

func TestValidateAndCorrectWith(t *testing.T) {
	if !ValidateString("hello 世界") {
		t.Fatalf("valid UTF-8 rejected")
	}
	bad := []byte{'a', 0xff, 'b'}
	if Validate(bad) {
		t.Fatalf("invalid UTF-8 accepted")
	}
	corrected := CorrectWith(nil, bad, "?")
	if string(corrected) != "a?b" {
		t.Fatalf("CorrectWith = %q", corrected)
	}
	if !Validate(corrected) {
		t.Fatalf("CorrectWith output is not valid UTF-8")
	}
}

func TestValidateNilAndEmpty(t *testing.T) {
	if !Validate(nil) {
		t.Fatalf("Validate(nil) = false, want true")
	}
	if !Validate([]byte{}) {
		t.Fatalf("Validate(empty) = false, want true")
	}
	if !ValidateString("") {
		t.Fatalf("ValidateString(\"\") = false, want true")
	}
}

func TestValidateStringASCIIAndMultibyte(t *testing.T) {
	if !ValidateString("plain ascii") {
		t.Fatalf("ascii string rejected")
	}
	if !ValidateString("中文") {
		t.Fatalf("multibyte string rejected")
	}
	if ValidateString("a\xc0b") {
		t.Fatalf("invalid string accepted")
	}
}

func TestValidateInvalidBytes(t *testing.T) {
	if Validate([]byte{0xff}) {
		t.Fatalf("lone 0xff accepted")
	}
	if Validate([]byte{0xc0, 0x00}) {
		t.Fatalf("invalid continuation accepted")
	}
	if Validate([]byte("ok\xff")) {
		t.Fatalf("trailing 0xff accepted")
	}
}

func TestCorrectWithPreservesDstPrefix(t *testing.T) {
	dst := []byte("pre:")
	out := CorrectWith(dst, []byte{'a', 0xff, 'b'}, "?")
	if string(out) != "pre:a?b" {
		t.Fatalf("CorrectWith with prefix = %q", out)
	}
	// Original prefix must be preserved at the start of the returned slice.
	if string(out[:4]) != "pre:" {
		t.Fatalf("prefix lost = %q", out[:4])
	}
}

func TestCorrectWithAllInvalid(t *testing.T) {
	out := CorrectWith(nil, []byte{0xff, 0xfe, 0xfd}, "##")
	if string(out) != "######" {
		t.Fatalf("all-invalid CorrectWith = %q", out)
	}
	if !Validate(out) {
		t.Fatalf("all-invalid output not valid")
	}
}

func TestCorrectWithMultibyteReplacement(t *testing.T) {
	out := CorrectWith(nil, []byte{0xff, 'a', 0xc0}, "�")
	if string(out) != "�a�" {
		t.Fatalf("multibyte repl = %q", out)
	}
}

func TestCorrectWithValidInputUnchanged(t *testing.T) {
	src := []byte("hello 世界")
	out := CorrectWith(nil, src, "?")
	if string(out) != string(src) {
		t.Fatalf("valid input altered: %q", out)
	}
}

func TestCorrectWithEmptySrc(t *testing.T) {
	dst := []byte("prefix")
	out := CorrectWith(dst, nil, "?")
	if string(out) != "prefix" {
		t.Fatalf("empty src = %q, want \"prefix\"", out)
	}
}
