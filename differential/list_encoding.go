package differential

import "strconv"

// listEncodingCases pins the list OBJECT ENCODING transition, the place a store
// most easily diverges from Redis. With the shipped default list-max-listpack-size
// of -2 the rule is byte-only: a list stays a single listpack until its listpack
// byte size crosses the 8KB tier, with no entry-count cap and no per-element cap.
// A store that carries an old quicklist-at-128-entries default reports quicklist
// where Redis and Valkey still report listpack, so the short-but-many case below
// catches exactly that divergence. The encoding step is compared exactly, not with
// ToleranceEncoding, so a wrong default fails the case rather than passing quietly.
func listEncodingCases() []Case {
	// rpush builds an RPUSH of n elements each equal to val onto key l.
	rpush := func(n int, val string) Command {
		c := make(Command, 0, n+2)
		c = append(c, "RPUSH", "l")
		for i := 0; i < n; i++ {
			c = append(c, val)
		}
		return c
	}
	// big60 is a 60-byte element, distinct per index so the bytes are real.
	big60 := func(i int) string {
		s := "e" + strconv.Itoa(i)
		for len(s) < 60 {
			s += "x"
		}
		return s
	}
	manyBig := func(n int) Command {
		c := make(Command, 0, n+2)
		c = append(c, "RPUSH", "l")
		for i := 0; i < n; i++ {
			c = append(c, big60(i))
		}
		return c
	}

	return []Case{
		{
			// 200 single-byte elements is well past the old 128-entry quicklist
			// default yet a tiny listpack (~0.4KB), so the byte-only -2 rule keeps it
			// listpack. This is the case the 128-entry-default bug fails.
			Name: "list-encoding-many-short-stays-listpack",
			Steps: []Command{
				rpush(200, "x"),
				{"LLEN", "l"},
				{"OBJECT", "ENCODING", "l"},
			},
		},
		{
			// 300 sixty-byte elements is ~18KB of listpack, comfortably past the 8KB
			// tier, so every store promotes to quicklist.
			Name: "list-encoding-large-bytes-promotes-quicklist",
			Steps: []Command{
				manyBig(300),
				{"LLEN", "l"},
				{"OBJECT", "ENCODING", "l"},
			},
		},
		{
			// A positive list-max-listpack-size caps the entry count: 5 elements past
			// a cap of 4 promotes to quicklist. The case resets the directive to the
			// shipped default so it leaves no residue for later cases (the runner only
			// flushes the keyspace between cases, not the config).
			Name: "list-encoding-entry-cap-config",
			Steps: []Command{
				{"CONFIG", "SET", "list-max-listpack-size", "4"},
				{"RPUSH", "l", "a", "b", "c", "d", "e"},
				{"OBJECT", "ENCODING", "l"},
				{"CONFIG", "SET", "list-max-listpack-size", "-2"},
			},
		},
	}
}
