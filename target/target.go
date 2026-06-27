// Package target launches or connects to a server under test and hands the
// harness a respwire client for it. The three servers we compare are aki, real
// Redis (redis-server), and Valkey (valkey-server). They all speak the same wire
// and take similar flags, so one Spawn path covers all three.
//
// There are two ways to get a target:
//
//   - Spawn starts a fresh server process on a free port. It needs the binary on
//     PATH. If the binary is missing it returns ErrBinaryMissing so the harness
//     can skip cleanly.
//   - Connect attaches to an already running address. CI without binaries uses
//     this against whatever the operator points it at, and it is also how you run
//     the suite against a remote server.
package target

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tamnd/aki-compat/respwire"
)

// Kind names a server implementation.
type Kind string

const (
	KindAki    Kind = "aki"
	KindRedis  Kind = "redis"
	KindValkey Kind = "valkey"
)

// ErrBinaryMissing means the server binary for a Spawn target is not on PATH.
// The harness treats this as a skip, not a failure, so a machine without Redis
// installed still gets a green run.
var ErrBinaryMissing = errors.New("server binary not found on PATH")

// Target is a running server the harness can talk to. Addr is its host:port.
// Close stops a spawned process or closes a connected handle.
type Target struct {
	Kind  Kind
	Addr  string
	cmd   *exec.Cmd
	tmp   string // temp data dir for a spawned process, removed on Close
	owned bool   // true if we started the process and must stop it
}

// Spec describes a target the operator wants. Exactly one of Spawn or Addr is
// used: if Addr is set we connect to it, otherwise we spawn a fresh process.
type Spec struct {
	Kind   Kind
	Addr   string // connect to this running address instead of spawning
	Binary string // override the binary name (defaults per Kind)
}

// binaryFor returns the default binary name for a kind.
func binaryFor(k Kind) string {
	switch k {
	case KindAki:
		return "aki"
	case KindRedis:
		return "redis-server"
	case KindValkey:
		return "valkey-server"
	default:
		return string(k)
	}
}

// Open resolves a Spec to a running Target. It connects when Addr is set and
// spawns otherwise.
func Open(spec Spec) (*Target, error) {
	if spec.Addr != "" {
		return Connect(spec.Kind, spec.Addr)
	}
	return Spawn(spec)
}

// Connect attaches to an already running server at addr. It does not own the
// process, so Close only drops the handle.
func Connect(kind Kind, addr string) (*Target, error) {
	if err := waitListening(addr, 2*time.Second); err != nil {
		return nil, fmt.Errorf("connect %s at %s: %w", kind, addr, err)
	}
	return &Target{Kind: kind, Addr: addr, owned: false}, nil
}

// Spawn starts a fresh server process on a free loopback port with a private
// temp data dir. It returns ErrBinaryMissing when the binary is not installed.
func Spawn(spec Spec) (*Target, error) {
	bin := spec.Binary
	if bin == "" {
		bin = binaryFor(spec.Kind)
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", bin, ErrBinaryMissing)
	}

	port, err := freePort()
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	tmp, err := os.MkdirTemp("", "aki-compat-"+string(spec.Kind)+"-")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(resolved, argsFor(spec.Kind, port, tmp)...)
	cmd.Dir = tmp
	cmd.Stdout = os.Stderr // server logs are diagnostic, keep them off stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}

	t := &Target{Kind: spec.Kind, Addr: addr, cmd: cmd, tmp: tmp, owned: true}
	if err := waitListening(addr, 10*time.Second); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("%s did not start listening: %w", spec.Kind, err)
	}
	return t, nil
}

// argsFor returns the command line that starts a kind on the given port with its
// data confined to dir. We bind loopback only and disable persistence so a test
// run leaves nothing behind.
func argsFor(k Kind, port int, dir string) []string {
	p := fmt.Sprintf("%d", port)
	switch k {
	case KindAki:
		return []string{
			"server",
			"--addr", "127.0.0.1:" + p,
			"--dbfile", filepath.Join(dir, "aki.db"),
			// Turn off the diagnostic admin endpoint. It defaults to a fixed
			// 127.0.0.1:6399, so several test instances spawned back to back would
			// otherwise fight over that one port instead of their own free ports.
			"--admin-port", "0",
		}
	default: // redis-server and valkey-server share the same flags
		return []string{
			"--port", p,
			"--bind", "127.0.0.1",
			"--save", "",
			"--appendonly", "no",
			"--dir", dir,
		}
	}
}

// Client dials the target and returns a fresh RESP client. If proto is 3 it runs
// HELLO 3 first so the connection speaks RESP3.
func (t *Target) Client(proto int) (*respwire.Client, error) {
	c, err := respwire.Dial(t.Addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if proto == 3 {
		if _, err := c.Hello(3); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close stops a spawned process and removes its temp dir. For a connected target
// it does nothing beyond dropping ownership.
func (t *Target) Close() error {
	if !t.owned {
		return nil
	}
	var firstErr error
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_, _ = t.cmd.Process.Wait()
	}
	if t.tmp != "" {
		if err := os.RemoveAll(t.tmp); err != nil {
			firstErr = err
		}
	}
	return firstErr
}

// Available reports whether a Spec can be opened right now. It is the check the
// harness uses to decide whether to skip a target. For a connect Spec it pings
// the address; for a spawn Spec it checks the binary is on PATH.
func Available(spec Spec) bool {
	if spec.Addr != "" {
		return waitListening(spec.Addr, 1*time.Second) == nil
	}
	bin := spec.Binary
	if bin == "" {
		bin = binaryFor(spec.Kind)
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitListening(addr string, within time.Duration) error {
	deadline := time.Now().Add(within)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("timed out")
	}
	return lastErr
}
