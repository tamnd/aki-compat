// Package respwire is a tiny native RESP client for the differential harness.
// It speaks RESP2 and RESP3, sends raw commands, and decodes the exact reply
// into a Value tree the harness can compare across servers byte for byte.
//
// The harness does not lean on a third-party client for the differential path.
// We want full control over framing so we can tell apart a $-1 null bulk from a
// _ RESP3 null, an empty array from a null array, and a simple string from a
// bulk string. A general purpose client hides those distinctions, and they are
// exactly the things a compatibility test must check.
package respwire

import (
	"fmt"
	"sort"
	"strings"
)

// Kind tags a decoded reply by its RESP leading byte. The values are the bytes
// themselves so a reader can switch on the wire byte and a debugger prints a
// readable character.
type Kind byte

const (
	KindSimpleString Kind = '+'
	KindError        Kind = '-'
	KindInteger      Kind = ':'
	KindBulkString   Kind = '$'
	KindArray        Kind = '*'

	// RESP3 additions.
	KindNull      Kind = '_'
	KindBool      Kind = '#'
	KindDouble    Kind = ','
	KindBigNumber Kind = '('
	KindBulkError Kind = '!'
	KindVerbatim  Kind = '='
	KindMap       Kind = '%'
	KindSet       Kind = '~'
	KindPush      Kind = '>'
)

// Value is a decoded RESP reply. Only the fields that match Kind carry meaning.
// Null bulk strings and null arrays decode to KindNull with the original leading
// byte kept in NullFrom so a strict comparison can still tell them apart when it
// wants to.
type Value struct {
	Kind     Kind
	Str      string  // simple string, bulk string, verbatim payload, error text
	Int      int64   // integer
	Bool     bool    // boolean
	Double   string  // double, kept as wire text to avoid float reformatting
	Big      string  // big number digits
	VerbEnc  string  // 3-char verbatim encoding hint
	Elems    []Value // array, set, push
	Map      [][2]Value
	NullFrom byte // for a null reply, the leading byte it arrived as ($, *, or _)
}

// IsError reports whether the reply is an error of either RESP form.
func (v Value) IsError() bool {
	return v.Kind == KindError || v.Kind == KindBulkError
}

// String renders a value in a stable, human readable form for diff output. It is
// not the wire form; it is meant to be read in a failure report.
func (v Value) String() string {
	switch v.Kind {
	case KindSimpleString:
		return "+" + v.Str
	case KindError, KindBulkError:
		return "-" + v.Str
	case KindInteger:
		return fmt.Sprintf(":%d", v.Int)
	case KindBulkString:
		return fmt.Sprintf("$%q", v.Str)
	case KindNull:
		return "(null)"
	case KindBool:
		if v.Bool {
			return "#t"
		}
		return "#f"
	case KindDouble:
		return "," + v.Double
	case KindBigNumber:
		return "(" + v.Big
	case KindVerbatim:
		return fmt.Sprintf("=%s:%q", v.VerbEnc, v.Str)
	case KindArray, KindSet, KindPush:
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			parts[i] = e.String()
		}
		open := map[Kind]string{KindArray: "[", KindSet: "{", KindPush: ">["}[v.Kind]
		shut := map[Kind]string{KindArray: "]", KindSet: "}", KindPush: "]"}[v.Kind]
		return open + strings.Join(parts, " ") + shut
	case KindMap:
		parts := make([]string, len(v.Map))
		for i, p := range v.Map {
			parts[i] = p[0].String() + "=>" + p[1].String()
		}
		return "%{" + strings.Join(parts, " ") + "}"
	default:
		return fmt.Sprintf("?%c", byte(v.Kind))
	}
}

// Normalize applies the documented normalizations that make two semantically
// equal replies from different servers compare equal. It does not mutate v.
//
// The normalizations are intentionally narrow:
//
//   - RESP2 and RESP3 carry the same logical reply in different shapes. A RESP2
//     map is a flat array of pairs and a RESP3 map is %. A RESP2 set is a plain
//     array and a RESP3 set is ~. We fold maps and sets to a canonical map and
//     set so a RESP2 target and a RESP3 target agree.
//   - Sets are unordered, so we sort their members.
//   - Maps are unordered by key, so we sort pairs by the canonical key form.
//
// Everything else is left exact. We deliberately do not paper over differing
// integers, differing strings, or differing error text. Those are real
// compatibility signals.
func (v Value) Normalize(opts NormalizeOptions) Value {
	switch v.Kind {
	case KindArray, KindPush:
		out := v
		out.Elems = make([]Value, len(v.Elems))
		for i, e := range v.Elems {
			out.Elems[i] = e.Normalize(opts)
		}
		return out
	case KindSet:
		out := v
		out.Kind = KindSet
		out.Elems = make([]Value, len(v.Elems))
		for i, e := range v.Elems {
			out.Elems[i] = e.Normalize(opts)
		}
		if opts.SortSets {
			sort.Slice(out.Elems, func(i, j int) bool {
				return out.Elems[i].String() < out.Elems[j].String()
			})
		}
		return out
	case KindMap:
		out := v
		out.Map = make([][2]Value, len(v.Map))
		for i, p := range v.Map {
			out.Map[i] = [2]Value{p[0].Normalize(opts), p[1].Normalize(opts)}
		}
		if opts.SortMaps {
			sort.Slice(out.Map, func(i, j int) bool {
				return out.Map[i][0].String() < out.Map[j][0].String()
			})
		}
		return out
	default:
		return v
	}
}

// NormalizeOptions selects which normalizations Normalize applies.
type NormalizeOptions struct {
	SortSets bool
	SortMaps bool
}

// DefaultNormalize sorts both sets and maps, which is what the differential
// suite wants for unordered replies.
func DefaultNormalize() NormalizeOptions {
	return NormalizeOptions{SortSets: true, SortMaps: true}
}

// Equal reports whether two values are equal after both are normalized with the
// same options. It is the comparison the differential model uses.
func Equal(a, b Value, opts NormalizeOptions) bool {
	return equalRaw(a.Normalize(opts), b.Normalize(opts))
}

func equalRaw(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindSimpleString, KindBulkString, KindError, KindBulkError:
		return a.Str == b.Str
	case KindInteger:
		return a.Int == b.Int
	case KindBool:
		return a.Bool == b.Bool
	case KindDouble:
		return a.Double == b.Double
	case KindBigNumber:
		return a.Big == b.Big
	case KindNull:
		return true
	case KindVerbatim:
		return a.VerbEnc == b.VerbEnc && a.Str == b.Str
	case KindArray, KindSet, KindPush:
		if len(a.Elems) != len(b.Elems) {
			return false
		}
		for i := range a.Elems {
			if !equalRaw(a.Elems[i], b.Elems[i]) {
				return false
			}
		}
		return true
	case KindMap:
		if len(a.Map) != len(b.Map) {
			return false
		}
		for i := range a.Map {
			if !equalRaw(a.Map[i][0], b.Map[i][0]) || !equalRaw(a.Map[i][1], b.Map[i][1]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
