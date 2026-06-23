package respwire

import (
	"bufio"
	"strings"
	"testing"
)

func decodeString(t *testing.T, wire string) Value {
	t.Helper()
	v, err := decode(bufio.NewReader(strings.NewReader(wire)))
	if err != nil {
		t.Fatalf("decode %q: %v", wire, err)
	}
	return v
}

func TestDecodeScalars(t *testing.T) {
	cases := []struct {
		wire string
		want Value
	}{
		{"+OK\r\n", Value{Kind: KindSimpleString, Str: "OK"}},
		{"-ERR bad\r\n", Value{Kind: KindError, Str: "ERR bad"}},
		{":42\r\n", Value{Kind: KindInteger, Int: 42}},
		{"$3\r\nfoo\r\n", Value{Kind: KindBulkString, Str: "foo"}},
		{"$0\r\n\r\n", Value{Kind: KindBulkString, Str: ""}},
		{"$-1\r\n", Value{Kind: KindNull, NullFrom: '$'}},
		{"*-1\r\n", Value{Kind: KindNull, NullFrom: '*'}},
		{"_\r\n", Value{Kind: KindNull, NullFrom: '_'}},
		{"#t\r\n", Value{Kind: KindBool, Bool: true}},
		{"#f\r\n", Value{Kind: KindBool, Bool: false}},
		{",3.14\r\n", Value{Kind: KindDouble, Double: "3.14"}},
		{"(12345\r\n", Value{Kind: KindBigNumber, Big: "12345"}},
	}
	for _, c := range cases {
		got := decodeString(t, c.wire)
		if got.Kind != c.want.Kind || got.Str != c.want.Str || got.Int != c.want.Int ||
			got.Bool != c.want.Bool || got.Double != c.want.Double || got.Big != c.want.Big ||
			got.NullFrom != c.want.NullFrom {
			t.Errorf("decode %q = %+v, want %+v", c.wire, got, c.want)
		}
	}
}

func TestDecodeArray(t *testing.T) {
	v := decodeString(t, "*2\r\n$3\r\nfoo\r\n:7\r\n")
	if v.Kind != KindArray || len(v.Elems) != 2 {
		t.Fatalf("array decode wrong: %+v", v)
	}
	if v.Elems[0].Str != "foo" || v.Elems[1].Int != 7 {
		t.Errorf("array elements wrong: %+v", v.Elems)
	}
}

func TestDecodeMap(t *testing.T) {
	v := decodeString(t, "%1\r\n$1\r\na\r\n:1\r\n")
	if v.Kind != KindMap || len(v.Map) != 1 {
		t.Fatalf("map decode wrong: %+v", v)
	}
	if v.Map[0][0].Str != "a" || v.Map[0][1].Int != 1 {
		t.Errorf("map pair wrong: %+v", v.Map[0])
	}
}

func TestDecodeVerbatim(t *testing.T) {
	v := decodeString(t, "=15\r\ntxt:Some string\r\n")
	if v.Kind != KindVerbatim || v.VerbEnc != "txt" || v.Str != "Some string" {
		t.Errorf("verbatim decode wrong: %+v", v)
	}
}
