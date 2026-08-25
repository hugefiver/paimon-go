package ast

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type sentinelVisitor struct {
	objectBeginErr error
	objectKeyErr   error
	objectEndErr   error
	arrayBeginErr  error
	arrayEndErr    error
	nullErr        error
	boolErr        error
	stringErr      error
	intErr         error
	floatErr       error
	events         []string
}

func (v *sentinelVisitor) OnNull() error {
	v.events = append(v.events, "null")
	return v.nullErr
}

func (v *sentinelVisitor) OnBool(bool) error {
	v.events = append(v.events, "bool")
	return v.boolErr
}

func (v *sentinelVisitor) OnString(string) error {
	v.events = append(v.events, "string")
	return v.stringErr
}

func (v *sentinelVisitor) OnInt64(int64, json.Number) error {
	v.events = append(v.events, "int")
	return v.intErr
}

func (v *sentinelVisitor) OnFloat64(float64, json.Number) error {
	v.events = append(v.events, "float")
	return v.floatErr
}

func (v *sentinelVisitor) OnObjectBegin(int) error {
	v.events = append(v.events, "object-begin")
	return v.objectBeginErr
}

func (v *sentinelVisitor) OnObjectKey(string) error {
	v.events = append(v.events, "object-key")
	return v.objectKeyErr
}

func (v *sentinelVisitor) OnObjectEnd() error {
	v.events = append(v.events, "object-end")
	return v.objectEndErr
}

func (v *sentinelVisitor) OnArrayBegin(int) error {
	v.events = append(v.events, "array-begin")
	return v.arrayBeginErr
}

func (v *sentinelVisitor) OnArrayEnd() error {
	v.events = append(v.events, "array-end")
	return v.arrayEndErr
}

func TestPreorderVisitOPSkipBeginCompatibility(t *testing.T) {
	objectBeginErr := fmt.Errorf("wrapped object begin: %w", VisitOPSkip)
	arrayBeginErr := fmt.Errorf("wrapped array begin: %w", VisitOPSkip)
	for _, tt := range []struct {
		name    string
		input   string
		visitor *sentinelVisitor
		want    error
	}{
		{
			name:    "wrapped object begin propagates unchanged",
			input:   `{"child":1}`,
			visitor: &sentinelVisitor{objectBeginErr: objectBeginErr},
			want:    objectBeginErr,
		},
		{
			name:    "wrapped array begin propagates unchanged",
			input:   `[1]`,
			visitor: &sentinelVisitor{arrayBeginErr: arrayBeginErr},
			want:    arrayBeginErr,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := Preorder(tt.input, tt.visitor, nil); err != tt.want {
				t.Fatalf("Preorder() error = %v, want original %v", err, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		input   string
		visitor *sentinelVisitor
		want    []string
	}{
		{
			name:    "object begin skips children and still ends",
			input:   `{"child":[1]}`,
			visitor: &sentinelVisitor{objectBeginErr: VisitOPSkip},
			want:    []string{"object-begin", "object-end"},
		},
		{
			name:    "array begin skips children and still ends",
			input:   `[{"child":1}]`,
			visitor: &sentinelVisitor{arrayBeginErr: VisitOPSkip},
			want:    []string{"array-begin", "array-end"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := Preorder(tt.input, tt.visitor, nil); err != nil {
				t.Fatalf("Preorder() error = %v", err)
			}
			if !reflect.DeepEqual(tt.visitor.events, tt.want) {
				t.Fatalf("events = %#v, want %#v", tt.visitor.events, tt.want)
			}
		})
	}
}

func TestPreorderNeverConsumesCallbackErrors(t *testing.T) {
	nullErr := fmt.Errorf("wrapped null: %w", VisitOPSkip)
	boolErr := fmt.Errorf("wrapped bool: %w", VisitOPSkip)
	stringErr := fmt.Errorf("wrapped string: %w", VisitOPSkip)
	intErr := fmt.Errorf("wrapped int: %w", VisitOPSkip)
	floatErr := fmt.Errorf("wrapped float: %w", VisitOPSkip)
	objectEndErr := fmt.Errorf("wrapped object end: %w", VisitOPSkip)
	arrayEndErr := fmt.Errorf("wrapped array end: %w", VisitOPSkip)
	for _, tt := range []struct {
		name    string
		input   string
		visitor *sentinelVisitor
		want    error
	}{
		{
			name:    "object key direct sentinel",
			input:   `{"key":1}`,
			visitor: &sentinelVisitor{objectKeyErr: VisitOPSkip},
			want:    VisitOPSkip,
		},
		{
			name:    "null wrapped sentinel",
			input:   `null`,
			visitor: &sentinelVisitor{nullErr: nullErr},
			want:    nullErr,
		},
		{
			name:    "bool wrapped sentinel",
			input:   `true`,
			visitor: &sentinelVisitor{boolErr: boolErr},
			want:    boolErr,
		},
		{
			name:    "string wrapped sentinel",
			input:   `"value"`,
			visitor: &sentinelVisitor{stringErr: stringErr},
			want:    stringErr,
		},
		{
			name:    "int wrapped sentinel",
			input:   `1`,
			visitor: &sentinelVisitor{intErr: intErr},
			want:    intErr,
		},
		{
			name:    "float wrapped sentinel",
			input:   `1.5`,
			visitor: &sentinelVisitor{floatErr: floatErr},
			want:    floatErr,
		},
		{
			name:    "object end wrapped sentinel",
			input:   `{}`,
			visitor: &sentinelVisitor{objectEndErr: objectEndErr},
			want:    objectEndErr,
		},
		{
			name:    "array end wrapped sentinel",
			input:   `[]`,
			visitor: &sentinelVisitor{arrayEndErr: arrayEndErr},
			want:    arrayEndErr,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := Preorder(tt.input, tt.visitor, nil); err != tt.want {
				t.Fatalf("Preorder() error = %v, want original %v", err, tt.want)
			}
		})
	}
}
