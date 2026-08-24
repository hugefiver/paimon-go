package main

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestReadRequest(t *testing.T) {
	valid := []byte(`{"data":"bnVsbA==","path":[{"kind":"key","key":"x"}]}`)
	want := request{Data: "bnVsbA==", Path: []pathPart{{Kind: "key", Key: "x"}}}
	got, err := readRequest(bytes.NewReader(valid))
	if err != nil {
		t.Fatalf("read valid request: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read valid request = %+v, want %+v", got, want)
	}

	for _, size := range []int{maxRequestBytes, maxRequestBytes + 1} {
		t.Run("size_"+strconv.Itoa(size), func(t *testing.T) {
			_, err := readRequest(strings.NewReader(strings.Repeat(" ", size)))
			if size == maxRequestBytes && errors.Is(err, errRequestTooLarge) {
				t.Fatalf("read %d-byte request returned errRequestTooLarge", size)
			}
			if size > maxRequestBytes && !errors.Is(err, errRequestTooLarge) {
				t.Fatalf("read %d-byte request error = %v, want errRequestTooLarge", size, err)
			}
		})
	}
}
