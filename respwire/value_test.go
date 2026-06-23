package respwire

import "testing"

func TestEqualScalars(t *testing.T) {
	opts := DefaultNormalize()
	if !Equal(Value{Kind: KindInteger, Int: 1}, Value{Kind: KindInteger, Int: 1}, opts) {
		t.Error("equal integers should compare equal")
	}
	if Equal(Value{Kind: KindInteger, Int: 1}, Value{Kind: KindInteger, Int: 2}, opts) {
		t.Error("different integers should not compare equal")
	}
	// A simple string and a bulk string with the same text are NOT equal: they
	// are different RESP types and a compatibility test must notice.
	if Equal(Value{Kind: KindSimpleString, Str: "OK"}, Value{Kind: KindBulkString, Str: "OK"}, opts) {
		t.Error("simple and bulk strings must not compare equal")
	}
}

func TestEqualSetsUnordered(t *testing.T) {
	opts := DefaultNormalize()
	a := Value{Kind: KindSet, Elems: []Value{
		{Kind: KindBulkString, Str: "a"},
		{Kind: KindBulkString, Str: "b"},
	}}
	b := Value{Kind: KindSet, Elems: []Value{
		{Kind: KindBulkString, Str: "b"},
		{Kind: KindBulkString, Str: "a"},
	}}
	if !Equal(a, b, opts) {
		t.Error("sets with the same members in different order should be equal")
	}
}

func TestEqualMapsUnordered(t *testing.T) {
	opts := DefaultNormalize()
	a := Value{Kind: KindMap, Map: [][2]Value{
		{{Kind: KindBulkString, Str: "k1"}, {Kind: KindInteger, Int: 1}},
		{{Kind: KindBulkString, Str: "k2"}, {Kind: KindInteger, Int: 2}},
	}}
	b := Value{Kind: KindMap, Map: [][2]Value{
		{{Kind: KindBulkString, Str: "k2"}, {Kind: KindInteger, Int: 2}},
		{{Kind: KindBulkString, Str: "k1"}, {Kind: KindInteger, Int: 1}},
	}}
	if !Equal(a, b, opts) {
		t.Error("maps with the same pairs in different order should be equal")
	}
}

func TestEqualArraysOrdered(t *testing.T) {
	opts := DefaultNormalize()
	a := Value{Kind: KindArray, Elems: []Value{{Kind: KindInteger, Int: 1}, {Kind: KindInteger, Int: 2}}}
	b := Value{Kind: KindArray, Elems: []Value{{Kind: KindInteger, Int: 2}, {Kind: KindInteger, Int: 1}}}
	if Equal(a, b, opts) {
		t.Error("arrays are ordered, reordered elements must not be equal")
	}
}
