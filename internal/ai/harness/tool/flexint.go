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
	g, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("invalid integer %q: %w", s, err)
	}
	*f = FlexInt(int(g))
	return nil
}

// Value returns the int value, or def if the pointer is nil.
func (f *FlexInt) Value(def int) int {
	if f == nil {
		return def
	}
	return int(*f)
}
