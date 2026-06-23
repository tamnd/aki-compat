// Package differential is the heart of the harness. It holds a table of command
// cases and runs each one against two or three targets, then asserts the replies
// agree after normalization.
//
// A Case is a named sequence of commands run on one fresh connection per target.
// Running a sequence rather than a single command lets a case set up state (SET a
// key) and then probe it (GET, TYPE, OBJECT ENCODING) and check the whole reply
// trace agrees. The last command's reply is usually the interesting one, but the
// harness compares every step so a divergence shows up at the exact command that
// caused it.
package differential

import (
	"fmt"
	"time"

	"github.com/tamnd/aki-compat/respwire"
	"github.com/tamnd/aki-compat/target"
)

// Command is one RESP command: the verb and its arguments.
type Command []string

// Tolerance loosens the comparison for one step where an exact match would be
// wrong. There are only two reasons to use it, and both are documented on the
// case: a reply carries a nondeterministic field (a TTL counts down between the
// two servers' calls), or a reply carries free-form text that is compatible in
// meaning but not byte identical (an error message's trailing detail).
//
// Tolerance never relaxes the parts of a reply that matter for compatibility. An
// approximate integer still must be an integer of the same sign and close value;
// an error-prefix match still requires both replies to be errors with the same
// prefix word.
type Tolerance int

const (
	// ToleranceExact is the default: replies must match after normalization.
	ToleranceExact Tolerance = iota
	// ToleranceIntApprox accepts two integers that differ by at most one. It is
	// for TTL and PTTL style replies that tick down between the two calls.
	ToleranceIntApprox
	// ToleranceErrPrefix accepts two errors whose first space-delimited word
	// matches (for example both start with ERR). Redis and aki word the trailing
	// detail of some errors differently, which is not a wire compatibility break.
	ToleranceErrPrefix
	// ToleranceUnordered sorts both array replies before comparing. On RESP2 the
	// set commands (SMEMBERS and friends) reply as a plain array, not a RESP3 set,
	// so the comparator cannot tell the order is unspecified. This says it is.
	ToleranceUnordered
	// ToleranceEncoding accepts any two non-error bulk string replies. OBJECT
	// ENCODING returns an internal encoding name that is implementation and
	// version specific (listpack vs quicklist, embstr vs raw), so two different
	// servers can legitimately disagree. We still assert both answered with a
	// bulk string rather than an error.
	ToleranceEncoding
)

// Case is a named scenario. Each command in Steps runs in order on the same
// connection. Cases must be self contained: they create the keys they touch and
// do not assume a clean database beyond what FLUSHDB at the start of a run gives
// them. The harness runs FLUSHDB before each case to isolate them.
type Case struct {
	Name  string
	Steps []Command
	// Proto picks the protocol version (2 or 3). RESP3 cases exercise map and set
	// reply shapes. Zero means RESP2.
	Proto int
	// Tolerate maps a step index to a documented relaxation of the comparison for
	// that step. A step not in the map is compared exactly.
	Tolerate map[int]Tolerance
}

// StepResult holds one command and the reply each target gave for it.
type StepResult struct {
	Command Command
	Replies map[target.Kind]respwire.Value
}

// CaseResult is the outcome of running a case against a set of targets.
type CaseResult struct {
	Case  Case
	Steps []StepResult
	Pass  bool
	// Mismatch points at the first step where targets disagreed, or -1 if none.
	Mismatch int
	// Err is set when the run could not complete (a dial failed, a server hung).
	Err error
}

// Runner runs cases against a fixed set of opened targets. The baseline target
// is the one every other target is compared against; by convention it is Redis
// when present, otherwise the first target. aki is then checked to match it.
type Runner struct {
	targets  []*target.Target
	baseline target.Kind
	opts     respwire.NormalizeOptions
	timeout  time.Duration
}

// NewRunner builds a runner over already opened targets. baseline names which
// target is the source of truth; if it is not among targets the first target is
// used.
func NewRunner(targets []*target.Target, baseline target.Kind) *Runner {
	return &Runner{
		targets:  targets,
		baseline: baseline,
		opts:     respwire.DefaultNormalize(),
		timeout:  5 * time.Second,
	}
}

// Targets reports the kinds the runner will exercise, in order.
func (r *Runner) Targets() []target.Kind {
	kinds := make([]target.Kind, len(r.targets))
	for i, t := range r.targets {
		kinds[i] = t.Kind
	}
	return kinds
}

// baselineKind returns the kind every other target is compared to.
func (r *Runner) baselineKind() target.Kind {
	for _, t := range r.targets {
		if t.Kind == r.baseline {
			return r.baseline
		}
	}
	if len(r.targets) > 0 {
		return r.targets[0].Kind
	}
	return ""
}

// Run executes one case against every target and decides pass or fail. A case
// passes when, at every step, all targets return replies equal to the baseline
// after normalization.
func (r *Runner) Run(c Case) CaseResult {
	res := CaseResult{Case: c, Mismatch: -1, Pass: true}

	clients := make(map[target.Kind]*respwire.Client, len(r.targets))
	for _, t := range r.targets {
		cl, err := t.Client(c.Proto)
		if err != nil {
			res.Err = fmt.Errorf("dial %s: %w", t.Kind, err)
			res.Pass = false
			closeClients(clients)
			return res
		}
		// Isolate the case on a clean database.
		if _, err := cl.Do("FLUSHDB"); err != nil {
			res.Err = fmt.Errorf("flushdb on %s: %w", t.Kind, err)
			res.Pass = false
			_ = cl.Close()
			closeClients(clients)
			return res
		}
		clients[t.Kind] = cl
	}
	defer closeClients(clients)

	base := r.baselineKind()

	for stepIdx, cmd := range c.Steps {
		step := StepResult{Command: cmd, Replies: make(map[target.Kind]respwire.Value, len(clients))}
		for kind, cl := range clients {
			_ = cl.SetDeadline(time.Now().Add(r.timeout))
			v, err := cl.Do(cmd...)
			if err != nil {
				res.Err = fmt.Errorf("%s step %d %v: %w", kind, stepIdx, cmd, err)
				res.Pass = false
				res.Steps = append(res.Steps, step)
				return res
			}
			step.Replies[kind] = v
		}
		res.Steps = append(res.Steps, step)

		// Compare every non-baseline target to the baseline at this step.
		baseVal, ok := step.Replies[base]
		if !ok {
			continue
		}
		tol := c.Tolerate[stepIdx]
		for kind, v := range step.Replies {
			if kind == base {
				continue
			}
			if !r.match(baseVal, v, tol) {
				res.Pass = false
				if res.Mismatch == -1 {
					res.Mismatch = stepIdx
				}
			}
		}
	}
	return res
}

// match compares a target reply to the baseline reply for one step, honoring the
// step's tolerance. ToleranceExact is the normal normalized comparison.
func (r *Runner) match(base, got respwire.Value, tol Tolerance) bool {
	switch tol {
	case ToleranceIntApprox:
		if base.Kind != respwire.KindInteger || got.Kind != respwire.KindInteger {
			return false
		}
		d := base.Int - got.Int
		if d < 0 {
			d = -d
		}
		return d <= 1
	case ToleranceErrPrefix:
		if !base.IsError() || !got.IsError() {
			return false
		}
		return firstWord(base.Str) == firstWord(got.Str)
	case ToleranceUnordered:
		if base.Kind != respwire.KindArray || got.Kind != respwire.KindArray {
			return false
		}
		// Reuse the set comparison, which sorts members, by retagging both as sets.
		b := base
		b.Kind = respwire.KindSet
		g := got
		g.Kind = respwire.KindSet
		return respwire.Equal(b, g, r.opts)
	case ToleranceEncoding:
		return base.Kind == respwire.KindBulkString && got.Kind == respwire.KindBulkString
	default:
		return respwire.Equal(base, got, r.opts)
	}
}

func firstWord(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}

func closeClients(clients map[target.Kind]*respwire.Client) {
	for _, cl := range clients {
		_ = cl.Close()
	}
}
