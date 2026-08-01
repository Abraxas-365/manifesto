package tool

import (
	"bytes"
	"fmt"
	"strconv"
)

// FlexInt is an int that tolerates JSON numbers, quoted numbers ("300"),
// floats (300.0), and null/empty. LLMs frequently emit numeric tool arguments
// as strings; accepting both shapes avoids spurious unmarshal failures that
// otherwise trap the model in a retry loop.
type FlexInt int

// UnmarshalJSON accepts 300, "300", 300.0, "", and null.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		data = data[1 : len(data)-1]
	}
	s := string(bytes.TrimSpace(data))
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*f = FlexInt(n)
		return nil
	}
	if g, err := strconv.ParseFloat(s, 64); err == nil {
		*f = FlexInt(int(g))
		return nil
	}
	// LLMs occasionally pass a range like "596, 675" or "596-685" (mirroring
	// the "file:596-685" line-range notation) where a single number is
	// expected. Fall back to the leading number rather than erroring out.
	if n, ok := leadingInt(s); ok {
		*f = FlexInt(n)
		return nil
	}
	return fmt.Errorf("invalid integer %q", s)
}

// leadingInt extracts the leading (optionally signed) integer from s, if any.
func leadingInt(s string) (int, bool) {
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, false
	}
	return n, true
}

// Value returns the int value, or def if the pointer is nil.
func (f *FlexInt) Value(def int) int {
	if f == nil {
		return def
	}
	return int(*f)
}
