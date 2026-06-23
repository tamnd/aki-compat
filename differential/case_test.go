package differential

import (
	"testing"

	"github.com/tamnd/aki-compat/respwire"
)

func TestMatchIntApprox(t *testing.T) {
	r := &Runner{opts: respwire.DefaultNormalize()}
	hundred := respwire.Value{Kind: respwire.KindInteger, Int: 100}
	ninetyNine := respwire.Value{Kind: respwire.KindInteger, Int: 99}
	ninetyEight := respwire.Value{Kind: respwire.KindInteger, Int: 98}

	if !r.match(hundred, ninetyNine, ToleranceIntApprox) {
		t.Error("100 and 99 should match within one")
	}
	if r.match(hundred, ninetyEight, ToleranceIntApprox) {
		t.Error("100 and 98 differ by two, should not match")
	}
	if r.match(hundred, ninetyNine, ToleranceExact) {
		t.Error("exact comparison must reject 100 vs 99")
	}
}

func TestMatchErrPrefix(t *testing.T) {
	r := &Runner{opts: respwire.DefaultNormalize()}
	a := respwire.Value{Kind: respwire.KindError, Str: "ERR unknown command 'X', detail a"}
	b := respwire.Value{Kind: respwire.KindError, Str: "ERR unknown command 'X', detail b"}
	c := respwire.Value{Kind: respwire.KindError, Str: "WRONGTYPE something"}

	if !r.match(a, b, ToleranceErrPrefix) {
		t.Error("two ERR errors should match on prefix")
	}
	if r.match(a, c, ToleranceErrPrefix) {
		t.Error("ERR and WRONGTYPE should not match on prefix")
	}
}

func TestMatchUnordered(t *testing.T) {
	r := &Runner{opts: respwire.DefaultNormalize()}
	a := respwire.Value{Kind: respwire.KindArray, Elems: []respwire.Value{
		{Kind: respwire.KindBulkString, Str: "a"},
		{Kind: respwire.KindBulkString, Str: "b"},
	}}
	b := respwire.Value{Kind: respwire.KindArray, Elems: []respwire.Value{
		{Kind: respwire.KindBulkString, Str: "b"},
		{Kind: respwire.KindBulkString, Str: "a"},
	}}
	if !r.match(a, b, ToleranceUnordered) {
		t.Error("reordered arrays should match unordered")
	}
	if r.match(a, b, ToleranceExact) {
		t.Error("reordered arrays must not match exactly")
	}
}

func TestMatchEncoding(t *testing.T) {
	r := &Runner{opts: respwire.DefaultNormalize()}
	quick := respwire.Value{Kind: respwire.KindBulkString, Str: "quicklist"}
	listpack := respwire.Value{Kind: respwire.KindBulkString, Str: "listpack"}
	errReply := respwire.Value{Kind: respwire.KindError, Str: "ERR no such key"}

	if !r.match(quick, listpack, ToleranceEncoding) {
		t.Error("two different encoding names should be tolerated")
	}
	if r.match(quick, errReply, ToleranceEncoding) {
		t.Error("an error reply must not pass the encoding tolerance")
	}
}

func TestMatchExactUsesNormalize(t *testing.T) {
	r := &Runner{opts: respwire.DefaultNormalize()}
	a := respwire.Value{Kind: respwire.KindBulkString, Str: "v"}
	b := respwire.Value{Kind: respwire.KindBulkString, Str: "v"}
	if !r.match(a, b, ToleranceExact) {
		t.Error("identical bulk strings should match exactly")
	}
}
