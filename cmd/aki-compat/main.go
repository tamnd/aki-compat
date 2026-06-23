// Command aki-compat runs the differential compatibility suite against the
// targets you point it at and prints a pass/fail report.
//
// Each target is either a running address you give it or a server it spawns from
// PATH. The differential model needs at least two live targets to compare. With
// fewer it prints what is available and exits without running cases.
//
// Examples:
//
//	# Spawn all three from PATH (needs aki, redis-server, valkey-server installed).
//	aki-compat
//
//	# Compare aki against an already running Redis.
//	aki-compat -aki-addr 127.0.0.1:7000 -redis-addr 127.0.0.1:6379
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/aki-compat/differential"
	"github.com/tamnd/aki-compat/target"
)

func main() {
	akiAddr := flag.String("aki-addr", "", "connect aki at this host:port instead of spawning")
	redisAddr := flag.String("redis-addr", "", "connect redis at this host:port instead of spawning")
	valkeyAddr := flag.String("valkey-addr", "", "connect valkey at this host:port instead of spawning")
	flag.Parse()

	if err := run(*akiAddr, *redisAddr, *valkeyAddr); err != nil {
		fmt.Fprintln(os.Stderr, "aki-compat:", err)
		os.Exit(1)
	}
}

func run(akiAddr, redisAddr, valkeyAddr string) error {
	specs := []target.Spec{
		{Kind: target.KindRedis, Addr: redisAddr},
		{Kind: target.KindValkey, Addr: valkeyAddr},
		{Kind: target.KindAki, Addr: akiAddr},
	}

	var opened []*target.Target
	for _, spec := range specs {
		if !target.Available(spec) {
			fmt.Printf("skip %-7s not available\n", spec.Kind)
			continue
		}
		tg, err := target.Open(spec)
		if err != nil {
			fmt.Printf("skip %-7s open failed: %v\n", spec.Kind, err)
			continue
		}
		defer tg.Close()
		fmt.Printf("ok   %-7s at %s\n", tg.Kind, tg.Addr)
		opened = append(opened, tg)
	}

	if len(opened) < 2 {
		return fmt.Errorf("need at least two live targets, have %d", len(opened))
	}

	baseline := opened[0].Kind
	for _, tg := range opened {
		if tg.Kind == target.KindRedis {
			baseline = target.KindRedis
			break
		}
	}

	runner := differential.NewRunner(opened, baseline)
	fmt.Printf("\ncomparing %v against baseline %s\n\n", runner.Targets(), baseline)

	pass, fail := 0, 0
	for _, c := range differential.Cases() {
		res := runner.Run(c)
		switch {
		case res.Err != nil:
			fail++
			fmt.Printf("ERROR %-28s %v\n", c.Name, res.Err)
		case res.Pass:
			pass++
			fmt.Printf("PASS  %s\n", c.Name)
		default:
			fail++
			step := res.Steps[res.Mismatch]
			fmt.Printf("FAIL  %-28s diverged at step %d %v\n", c.Name, res.Mismatch, step.Command)
			for _, kind := range runner.Targets() {
				if v, ok := step.Replies[kind]; ok {
					fmt.Printf("        %-7s %s\n", kind, v.String())
				}
			}
		}
	}

	fmt.Printf("\n%d passed, %d failed\n", pass, fail)
	if fail > 0 {
		return fmt.Errorf("%d case(s) failed", fail)
	}
	return nil
}
