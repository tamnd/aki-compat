package differential

// Cases returns the full differential suite. Each case is self contained and
// runs on a clean database (the runner flushes before every case).
//
// The set covers the core string, counter, expiry, generic, list, hash, set,
// and sorted set commands, plus the things compatibility work most often gets
// wrong: substring ops, OBJECT ENCODING, WRONGTYPE errors, and MULTI/EXEC
// framing. It is meant to grow; add a case by appending to this slice.
func Cases() []Case {
	return []Case{
		// Connection and echo.
		{
			Name:  "ping",
			Steps: []Command{{"PING"}, {"PING", "hello"}},
		},
		{
			Name:  "echo",
			Steps: []Command{{"ECHO", "a binary\x00value"}},
		},

		// Strings.
		{
			Name: "set-get",
			Steps: []Command{
				{"SET", "k", "v"},
				{"GET", "k"},
				{"GET", "missing"},
			},
		},
		{
			Name: "append",
			Steps: []Command{
				{"APPEND", "s", "foo"},
				{"APPEND", "s", "bar"},
				{"GET", "s"},
			},
		},
		{
			Name: "getrange-setrange",
			Steps: []Command{
				{"SET", "k", "Hello World"},
				{"GETRANGE", "k", "0", "4"},
				{"GETRANGE", "k", "-5", "-1"},
				{"SETRANGE", "k", "6", "Redis"},
				{"GET", "k"},
			},
		},

		// Counters.
		{
			Name: "incr",
			Steps: []Command{
				{"SET", "n", "10"},
				{"INCR", "n"},
				{"INCRBY", "n", "5"},
				{"DECR", "n"},
				{"GET", "n"},
			},
		},
		{
			// INCRBYFLOAT replies are bulk strings of the decimal result. We pick
			// values whose sum is exactly representable so Redis and aki print the
			// same digits; the goal here is the command's arithmetic and reply
			// shape, not float text formatting, which differs harmlessly between
			// implementations and is out of scope for a wire compatibility check.
			Name: "incrbyfloat",
			Steps: []Command{
				{"SET", "f", "10.5"},
				{"INCRBYFLOAT", "f", "0.25"},
				{"INCRBYFLOAT", "f", "-5"},
			},
		},
		{
			Name: "incr-not-an-integer",
			Steps: []Command{
				{"SET", "k", "notanumber"},
				{"INCR", "k"},
			},
		},

		// Expiry.
		{
			// The first TTL can read 100 or 99 depending on how the second's clock
			// fell relative to the EXPIRE, so step 2 tolerates an off-by-one. The
			// PERSIST result, the post-persist -1, and the missing-key -2 are exact.
			Name: "expire-ttl-persist",
			Steps: []Command{
				{"SET", "k", "v"},
				{"EXPIRE", "k", "100"},
				{"TTL", "k"},
				{"PERSIST", "k"},
				{"TTL", "k"},
				{"TTL", "missing"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox},
		},

		// Generic key ops.
		{
			Name: "del-exists-type",
			Steps: []Command{
				{"SET", "a", "1"},
				{"SET", "b", "2"},
				{"EXISTS", "a", "b", "missing"},
				{"TYPE", "a"},
				{"DEL", "a", "b", "missing"},
				{"EXISTS", "a"},
			},
		},

		// Lists.
		{
			Name: "list-push-range",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c"},
				{"LPUSH", "l", "z"},
				{"LRANGE", "l", "0", "-1"},
				{"LLEN", "l"},
			},
		},

		// Hashes.
		{
			Name: "hash-set-get",
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2"},
				{"HGET", "h", "f1"},
				{"HGET", "h", "missing"},
				{"HGETALL", "h"},
			},
		},
		{
			Name:  "hash-getall-resp3",
			Proto: 3,
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2"},
				{"HGETALL", "h"},
			},
		},

		// Sets.
		{
			Name: "set-add-members",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c", "a"},
				{"SCARD", "s"},
				{"SMEMBERS", "s"},
				{"SISMEMBER", "s", "b"},
				{"SISMEMBER", "s", "z"},
			},
		},
		{
			Name:  "smembers-resp3",
			Proto: 3,
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"SMEMBERS", "s"},
			},
		},

		// Sorted sets.
		{
			Name: "zset-add-range-score",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c"},
				{"ZRANGE", "z", "0", "-1"},
				{"ZRANGE", "z", "0", "-1", "WITHSCORES"},
				{"ZSCORE", "z", "b"},
				{"ZSCORE", "z", "missing"},
			},
		},

		// Encodings.
		{
			Name: "object-encoding-int",
			Steps: []Command{
				{"SET", "k", "12345"},
				{"OBJECT", "ENCODING", "k"},
			},
		},
		{
			Name: "object-encoding-list",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c"},
				{"OBJECT", "ENCODING", "l"},
			},
		},

		// WRONGTYPE error cases.
		{
			Name: "wrongtype-list-on-string",
			Steps: []Command{
				{"SET", "k", "v"},
				{"LPUSH", "k", "x"},
			},
		},
		{
			Name: "wrongtype-get-on-list",
			Steps: []Command{
				{"RPUSH", "k", "a"},
				{"GET", "k"},
			},
		},

		// Transactions.
		{
			Name: "multi-exec",
			Steps: []Command{
				{"MULTI"},
				{"SET", "k", "v"},
				{"INCR", "n"},
				{"EXEC"},
				{"GET", "k"},
				{"GET", "n"},
			},
		},
		{
			Name: "multi-discard",
			Steps: []Command{
				{"MULTI"},
				{"SET", "k", "v"},
				{"DISCARD"},
				{"EXISTS", "k"},
			},
		},

		// Arity error.
		{
			Name:  "wrong-arity",
			Steps: []Command{{"GET"}},
		},
		{
			// Both servers reject an unknown command with an ERR error. The trailing
			// "with args beginning with" detail is worded slightly differently, which
			// is not a wire compatibility break, so we match on the ERR prefix.
			Name:     "unknown-command",
			Steps:    []Command{{"NOSUCHCOMMAND", "x"}},
			Tolerate: map[int]Tolerance{0: ToleranceErrPrefix},
		},
	}
}
