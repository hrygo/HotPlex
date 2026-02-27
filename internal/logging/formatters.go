package logging

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// FloatFormat defines how floating point numbers should be formatted in logs.
type FloatFormat int

const (
	// FloatPrecise formats float64 values with 2 decimal places.
	FloatPrecise FloatFormat = iota
	// FloatRaw keeps float64 values as-is without rounding.
	FloatRaw
)

// FormatFloat formats a float64 value according to the specified format.
func FormatFloat(f float64, format FloatFormat) float64 {
	switch format {
	case FloatPrecise:
		return math.Round(f*100) / 100
	default:
		return f
	}
}

// DurationMs represents duration in milliseconds for logging.
type DurationMs int64

// ToDuration converts DurationMs to time.Duration.
func (d DurationMs) ToDuration() time.Duration {
	return time.Duration(d) * time.Millisecond
}

// FloatValue wraps a float64 with specific formatting intent.
type FloatValue struct {
	Value  float64
	Format FloatFormat
}

// NewFloatValue creates a new FloatValue with the given format.
func NewFloatValue(value float64, format FloatFormat) FloatValue {
	return FloatValue{
		Value:  FormatFloat(value, format),
		Format: format,
	}
}

// MarshalJSON implements json.Marshaler for FloatValue.
func (f FloatValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.Value)
}

// String implements fmt.Stringer for FloatValue.
func (f FloatValue) String() string {
	switch f.Format {
	case FloatPrecise:
		return fmt.Sprintf("%.2f", f.Value)
	default:
		return fmt.Sprintf("%v", f.Value)
	}
}

// CostUsd represents cost in USD for logging.
type CostUsd float64

// String implements fmt.Stringer for CostUsd.
func (c CostUsd) String() string {
	return fmt.Sprintf("%.6f", c)
}

// MarshalJSON implements json.Marshaler for CostUsd.
func (c CostUsd) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(c))
}
