package respwire

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"time"
)

// Client is a minimal synchronous RESP client. It sends one command at a time as
// a RESP array of bulk strings and reads back exactly one reply. It is not a
// connection pool and it is not safe for concurrent use; the harness uses one
// client per target per case, which keeps the wire trace easy to reason about.
type Client struct {
	conn  net.Conn
	r     *bufio.Reader
	w     *bufio.Writer
	proto int
}

// Dial opens a TCP connection to addr and returns a RESP2 client. Call Hello to
// switch a connection to RESP3.
func Dial(addr string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:  conn,
		r:     bufio.NewReader(conn),
		w:     bufio.NewWriter(conn),
		proto: 2,
	}, nil
}

// Proto reports the negotiated protocol version (2 or 3).
func (c *Client) Proto() int { return c.proto }

// Close closes the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// SetDeadline bounds the next request and reply. The harness sets a per-command
// deadline so a hung server fails the case instead of the whole run.
func (c *Client) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// Do sends one command and returns the decoded reply. args are the command name
// and its arguments, each encoded as a bulk string.
func (c *Client) Do(args ...string) (Value, error) {
	if err := c.encodeCommand(args); err != nil {
		return Value{}, err
	}
	if err := c.w.Flush(); err != nil {
		return Value{}, err
	}
	return decode(c.r)
}

// Hello runs HELLO with the given protocol version and records the result so
// later replies are decoded against the right shape expectations. It returns the
// HELLO reply so a caller can inspect it.
func (c *Client) Hello(proto int) (Value, error) {
	v, err := c.Do("HELLO", strconv.Itoa(proto))
	if err != nil {
		return v, err
	}
	if v.IsError() {
		return v, fmt.Errorf("HELLO %d: %s", proto, v.Str)
	}
	c.proto = proto
	return v, nil
}

func (c *Client) encodeCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("respwire: empty command")
	}
	if _, err := c.w.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, a := range args {
		if _, err := c.w.WriteString("$" + strconv.Itoa(len(a)) + "\r\n"); err != nil {
			return err
		}
		if _, err := c.w.WriteString(a); err != nil {
			return err
		}
		if _, err := c.w.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return nil
}
