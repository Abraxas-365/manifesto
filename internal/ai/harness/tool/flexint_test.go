package tool

import (
	"encoding/json"
	"testing"
)

func TestFlexInt_Unmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`300`, 300},
		{`"300"`, 300},
		{`300.0`, 300},
		{`"300.0"`, 300},
		{`0`, 0},
		{`""`, 0},
		{`null`, 0},
	}
	for _, c := range cases {
		var f FlexInt
		if err := json.Unmarshal([]byte(c.in), &f); err != nil {
			t.Errorf("Unmarshal(%s) error: %v", c.in, err)
			continue
		}
		if int(f) != c.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", c.in, int(f), c.want)
		}
	}
}

func TestFlexInt_Invalid(t *testing.T) {
	var f FlexInt
	if err := json.Unmarshal([]byte(`"abc"`), &f); err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestFlexInt_Value(t *testing.T) {
	var p *FlexInt
	if p.Value(7) != 7 {
		t.Fatal("nil FlexInt should return default")
	}
	f := FlexInt(3)
	if f.Value(7) != 3 {
		t.Fatal("set FlexInt should return its value")
	}
}

func TestFlexInt_InStruct(t *testing.T) {
	type in struct {
		Timeout FlexInt `json:"timeout"`
	}
	var v in
	if err := json.Unmarshal([]byte(`{"timeout":"1500"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Timeout.Value(0) != 1500 {
		t.Fatalf("got %d", v.Timeout.Value(0))
	}
}
