package differential_test

import (
	"os"
	"testing"

	"github.com/tamnd/aki-compat/differential"
	"github.com/tamnd/aki-compat/target"
)

// TestDifferential runs the full suite against whatever targets the environment
// makes available, and skips cleanly when there are not at least two.
//
// Target selection by env var, so CI can wire up running servers without
// installing binaries here:
//
//	AKI_COMPAT_AKI_ADDR     connect aki at this host:port
//	AKI_COMPAT_REDIS_ADDR   connect redis at this host:port
//	AKI_COMPAT_VALKEY_ADDR  connect valkey at this host:port
//
// If an addr is unset, the harness tries to spawn that server from PATH. A
// missing binary is a skip for that target, not a failure. The differential
// model needs at least two live targets to compare, so with zero or one the
// whole test skips and CI stays green.
func TestDifferential(t *testing.T) {
	specs := []target.Spec{
		{Kind: target.KindRedis, Addr: os.Getenv("AKI_COMPAT_REDIS_ADDR")},
		{Kind: target.KindValkey, Addr: os.Getenv("AKI_COMPAT_VALKEY_ADDR")},
		{Kind: target.KindAki, Addr: os.Getenv("AKI_COMPAT_AKI_ADDR")},
	}

	var opened []*target.Target
	for _, spec := range specs {
		if !target.Available(spec) {
			t.Logf("target %s not available, skipping it", spec.Kind)
			continue
		}
		tg, err := target.Open(spec)
		if err != nil {
			t.Logf("target %s could not open: %v, skipping it", spec.Kind, err)
			continue
		}
		t.Cleanup(func() { _ = tg.Close() })
		opened = append(opened, tg)
	}

	if len(opened) < 2 {
		t.Skip("need at least two live targets to run a differential comparison")
	}

	// Prefer Redis as the source of truth; otherwise the first opened target.
	baseline := opened[0].Kind
	for _, tg := range opened {
		if tg.Kind == target.KindRedis {
			baseline = target.KindRedis
			break
		}
	}

	runner := differential.NewRunner(opened, baseline)
	t.Logf("comparing %v against baseline %s", runner.Targets(), baseline)

	for _, c := range differential.Cases() {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			res := runner.Run(c)
			if res.Err != nil {
				t.Fatalf("case %q errored: %v", c.Name, res.Err)
			}
			if !res.Pass {
				step := res.Steps[res.Mismatch]
				t.Errorf("case %q diverged at step %d %v", c.Name, res.Mismatch, step.Command)
				for kind, v := range step.Replies {
					t.Errorf("  %-7s %s", kind, v.String())
				}
			}
		})
	}
}
