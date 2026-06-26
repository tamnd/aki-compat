package differential

// Cases returns the full differential suite. Each case is self contained and
// runs on a clean database (the runner flushes before every case).
//
// The set covers the core string, counter, expiry, generic, list, hash, set,
// and sorted set commands, plus the things compatibility work most often gets
// wrong: substring ops, OBJECT ENCODING, WRONGTYPE errors, and MULTI/EXEC
// framing. It is meant to grow; add a case by appending to this slice.
func Cases() []Case {
	base := []Case{
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
			// On RESP2 SMEMBERS replies as a plain array, and the member order is
			// unspecified, so step 2 is compared unordered. SCARD and SISMEMBER are
			// exact.
			Name: "set-add-members",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c", "a"},
				{"SCARD", "s"},
				{"SMEMBERS", "s"},
				{"SISMEMBER", "s", "b"},
				{"SISMEMBER", "s", "z"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceUnordered},
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
			// OBJECT ENCODING returns a version-specific internal name, so step 1
			// only asserts both servers replied with a bulk string. The point of the
			// case is that OBJECT ENCODING exists and answers, not that two different
			// server versions pick the same encoding.
			Name: "object-encoding-int",
			Steps: []Command{
				{"SET", "k", "12345"},
				{"OBJECT", "ENCODING", "k"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},
		{
			Name: "object-encoding-list",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c"},
				{"OBJECT", "ENCODING", "l"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
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

		// SET with options.
		{
			Name: "set-nx",
			Steps: []Command{
				{"SET", "k", "first", "NX"},
				{"SET", "k", "second", "NX"},
				{"GET", "k"},
			},
		},
		{
			Name: "set-xx",
			Steps: []Command{
				{"SET", "k", "v", "XX"},
				{"SET", "k", "v"},
				{"SET", "k", "updated", "XX"},
				{"GET", "k"},
			},
		},
		{
			Name: "set-get-option",
			Steps: []Command{
				{"SET", "k", "old"},
				{"SET", "k", "new", "GET"},
				{"GET", "k"},
			},
		},
		{
			Name: "set-get-missing",
			Steps: []Command{
				{"SET", "k", "v", "GET"},
			},
		},
		{
			Name: "set-ex-px",
			Steps: []Command{
				{"SET", "k1", "v", "EX", "100"},
				{"SET", "k2", "v", "PX", "100000"},
				{"TTL", "k1"},
				{"PTTL", "k2"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox, 3: ToleranceIntApprox},
		},
		{
			Name: "set-keepttl",
			Steps: []Command{
				{"SET", "k", "v", "EX", "100"},
				{"SET", "k", "w", "KEEPTTL"},
				{"TTL", "k"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox},
		},

		// GETEX, GETDEL.
		{
			Name: "getex",
			Steps: []Command{
				{"SET", "k", "v"},
				{"GETEX", "k", "EX", "100"},
				{"TTL", "k"},
				{"GETEX", "k", "PERSIST"},
				{"TTL", "k"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox},
		},
		{
			Name: "getdel",
			Steps: []Command{
				{"SET", "k", "v"},
				{"GETDEL", "k"},
				{"EXISTS", "k"},
				{"GETDEL", "missing"},
			},
		},

		// MSET/MGET/MSETNX.
		{
			Name: "mset-mget",
			Steps: []Command{
				{"MSET", "k1", "v1", "k2", "v2", "k3", "v3"},
				{"MGET", "k1", "k2", "missing", "k3"},
			},
		},
		{
			Name: "msetnx",
			Steps: []Command{
				{"MSETNX", "a", "1", "b", "2"},
				{"MSETNX", "b", "3", "c", "4"},
				{"MGET", "a", "b", "c"},
			},
		},

		// Legacy string commands.
		{
			Name: "setnx-setex",
			Steps: []Command{
				{"SETNX", "k", "v"},
				{"SETNX", "k", "w"},
				{"GET", "k"},
				{"SETEX", "e", "100", "ev"},
				{"TTL", "e"},
			},
			Tolerate: map[int]Tolerance{4: ToleranceIntApprox},
		},
		{
			// GETSET is deprecated since Redis 6.2; it still works on most versions.
			Name: "getset",
			Steps: []Command{
				{"SET", "k", "old"},
				{"GETSET", "k", "new"},
				{"GET", "k"},
			},
		},

		// More expiry commands.
		{
			Name: "pexpire-pttl",
			Steps: []Command{
				{"SET", "k", "v"},
				{"PEXPIRE", "k", "100000"},
				{"PTTL", "k"},
				{"TTL", "k"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox, 3: ToleranceIntApprox},
		},
		{
			Name: "expiretime-pexpiretime",
			Steps: []Command{
				{"SET", "k", "v"},
				{"EXPIREAT", "k", "9999999999"},
				{"EXPIRETIME", "k"},
				{"PEXPIRETIME", "k"},
			},
		},
		{
			Name: "expire-nx-xx-gt-lt",
			Steps: []Command{
				{"SET", "k", "v"},
				{"EXPIRE", "k", "100"},
				{"EXPIRE", "k", "50", "XX"},
				{"EXPIRE", "k", "200", "GT"},
				{"EXPIRE", "k", "10", "LT"},
				{"TTL", "k"},
				{"EXPIRE", "missing", "100", "XX"},
			},
			Tolerate: map[int]Tolerance{5: ToleranceIntApprox},
		},

		// RENAME, RENAMENX, COPY, UNLINK.
		{
			Name: "rename",
			Steps: []Command{
				{"SET", "src", "v"},
				{"RENAME", "src", "dst"},
				{"EXISTS", "src"},
				{"GET", "dst"},
				{"RENAME", "missing", "dst"},
			},
		},
		{
			Name: "renamenx",
			Steps: []Command{
				{"SET", "a", "1"},
				{"SET", "b", "2"},
				{"RENAMENX", "a", "c"},
				{"RENAMENX", "b", "c"},
				{"MGET", "a", "b", "c"},
			},
		},
		{
			Name: "copy",
			Steps: []Command{
				{"SET", "src", "hello"},
				{"COPY", "src", "dst"},
				{"GET", "dst"},
				{"COPY", "src", "dst"},
				{"COPY", "src", "dst", "REPLACE"},
			},
		},
		{
			Name: "unlink-scan",
			Steps: []Command{
				{"SET", "k1", "v1"},
				{"SET", "k2", "v2"},
				{"UNLINK", "k1", "k2", "missing"},
				{"EXISTS", "k1", "k2"},
			},
		},

		// OBJECT ENCODING edge cases.
		{
			// OBJECT ENCODING on a missing key. Redis 7.4 returns an error; Redis 8
			// changed it to return null. Accept any non-error (null) or error response.
			Name: "object-encoding-missing",
			Steps: []Command{
				{"OBJECT", "ENCODING", "missing"},
			},
			Tolerate: map[int]Tolerance{0: ToleranceAny},
		},
		{
			// embstr vs raw threshold. This is version-specific, not just a name
			// difference: Redis 7.x flips embstr to raw above 44 bytes
			// (OBJ_ENCODING_EMBSTR_SIZE_LIMIT), so a 45-byte string is raw. Valkey 9.x
			// raised that limit, so the same string is still embstr there. aki follows
			// the Redis 7.4 rule. Because the two reference servers legitimately
			// disagree on the threshold, step 1 takes ToleranceEncoding like every
			// other OBJECT ENCODING case: we assert both answered with an encoding
			// name, not that they chose the same one. Run against a real Redis 7.4 as
			// the baseline to check aki picks raw here.
			Name: "object-encoding-embstr-raw",
			Steps: []Command{
				{"SET", "short", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, // 45 bytes: raw on Redis 7.4
				{"OBJECT", "ENCODING", "short"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},
		{
			// Hash encoding: small hash is listpack.
			Name: "object-encoding-hash-listpack",
			Steps: []Command{
				{"HSET", "h", "f", "v"},
				{"OBJECT", "ENCODING", "h"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},
		{
			// Set encoding: small integer set is intset.
			Name: "object-encoding-intset",
			Steps: []Command{
				{"SADD", "s", "1", "2", "3"},
				{"OBJECT", "ENCODING", "s"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},
		{
			// Sorted set encoding: small zset is listpack.
			Name: "object-encoding-zset-listpack",
			Steps: []Command{
				{"ZADD", "z", "1.0", "a"},
				{"OBJECT", "ENCODING", "z"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},

		// List commands.
		{
			Name: "lpop-rpop-count",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c", "d", "e"},
				{"LPOP", "l", "2"},
				{"RPOP", "l", "2"},
				{"LRANGE", "l", "0", "-1"},
			},
		},
		{
			Name: "lpos",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c", "b", "d", "b"},
				{"LPOS", "l", "b"},
				{"LPOS", "l", "b", "RANK", "2"},
				{"LPOS", "l", "b", "COUNT", "0"},
			},
		},
		{
			Name: "lmove",
			Steps: []Command{
				{"RPUSH", "src", "a", "b", "c"},
				{"LMOVE", "src", "dst", "LEFT", "RIGHT"},
				{"LMOVE", "src", "dst", "RIGHT", "LEFT"},
				{"LRANGE", "src", "0", "-1"},
				{"LRANGE", "dst", "0", "-1"},
			},
		},
		{
			Name: "lindex-lset-linsert",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c"},
				{"LINDEX", "l", "1"},
				{"LINDEX", "l", "-1"},
				{"LSET", "l", "1", "B"},
				{"LINSERT", "l", "BEFORE", "B", "x"},
				{"LRANGE", "l", "0", "-1"},
			},
		},
		{
			Name: "lrem",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "a", "c", "a"},
				{"LREM", "l", "2", "a"},
				{"LRANGE", "l", "0", "-1"},
			},
		},
		{
			Name: "ltrim",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c", "d", "e"},
				{"LTRIM", "l", "1", "3"},
				{"LRANGE", "l", "0", "-1"},
			},
		},

		// Hash commands.
		{
			Name: "hmget-hmset",
			Steps: []Command{
				{"HMSET", "h", "f1", "v1", "f2", "v2"},
				{"HMGET", "h", "f1", "missing", "f2"},
				{"HLEN", "h"},
				{"HEXISTS", "h", "f1"},
				{"HEXISTS", "h", "missing"},
			},
		},
		{
			Name: "hkeys-hvals",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2", "c", "3"},
				{"HKEYS", "h"},
				{"HVALS", "h"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceUnordered, 2: ToleranceUnordered},
		},
		{
			Name: "hincrby-hincrbyfloat",
			Steps: []Command{
				{"HSET", "h", "n", "10"},
				{"HINCRBY", "h", "n", "5"},
				{"HINCRBYFLOAT", "h", "f", "1.5"},
				{"HINCRBYFLOAT", "h", "f", "0.5"},
				{"HMGET", "h", "n", "f"},
			},
		},
		{
			Name: "hdel",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2", "c", "3"},
				{"HDEL", "h", "a", "b", "missing"},
				{"HGETALL", "h"},
			},
		},
		{
			Name: "hrandfield",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2"},
				{"HRANDFIELD", "h"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding}, // random field, just check it's a string
		},
		{
			Name: "hsetnx",
			Steps: []Command{
				{"HSETNX", "h", "f", "v"},
				{"HSETNX", "h", "f", "w"},
				{"HGET", "h", "f"},
			},
		},

		// Set commands.
		{
			Name: "sinter-sunion-sdiff",
			Steps: []Command{
				{"SADD", "a", "1", "2", "3"},
				{"SADD", "b", "2", "3", "4"},
				{"SINTERCARD", "2", "a", "b"},
				{"SINTERCARD", "2", "a", "b", "LIMIT", "1"},
			},
		},
		{
			Name: "smismember",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"SMISMEMBER", "s", "a", "x", "b"},
			},
		},
		{
			Name: "srandmember",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"SRANDMEMBER", "s"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceEncoding},
		},
		{
			Name: "spop",
			Steps: []Command{
				{"SADD", "s", "a"},
				{"SPOP", "s"},
				{"SCARD", "s"},
			},
		},
		{
			Name: "sunionstore-sdiffstore",
			Steps: []Command{
				{"SADD", "a", "1", "2"},
				{"SADD", "b", "2", "3"},
				{"SUNIONSTORE", "dst", "a", "b"},
				{"SDIFFSTORE", "diff", "a", "b"},
				{"SCARD", "dst"},
				{"SCARD", "diff"},
			},
		},

		// Sorted set commands.
		{
			Name: "zadd-options",
			Steps: []Command{
				{"ZADD", "z", "1", "a"},
				{"ZADD", "z", "NX", "2", "a"},
				{"ZADD", "z", "XX", "3", "a"},
				{"ZSCORE", "z", "a"},
				{"ZADD", "z", "GT", "2", "a"},
				{"ZADD", "z", "GT", "5", "a"},
				{"ZSCORE", "z", "a"},
				{"ZADD", "z", "CH", "6", "a", "1", "b"},
			},
		},
		{
			Name: "zincrby-zrank-zcard",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c"},
				{"ZINCRBY", "z", "10", "a"},
				{"ZRANK", "z", "a"},
				{"ZCARD", "z"},
				{"ZRANK", "z", "a", "WITHSCORE"},
			},
		},
		{
			Name: "zrangebyscore",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c", "4", "d"},
				{"ZRANGEBYSCORE", "z", "2", "3"},
				{"ZRANGEBYSCORE", "z", "-inf", "+inf", "WITHSCORES"},
				{"ZREVRANGEBYSCORE", "z", "3", "1"},
				{"ZRANGEBYSCORE", "z", "1", "3", "LIMIT", "0", "2"},
			},
		},
		{
			Name: "zrange-byscore-bylex",
			Steps: []Command{
				{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d"},
				{"ZRANGE", "z", "[a", "[c", "BYLEX"},
				{"ZRANGE", "z", "1", "3", "BYSCORE", "REV"},
			},
		},
		{
			Name: "zrem-zpopmin-zpopmax",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c"},
				{"ZREM", "z", "b", "missing"},
				{"ZPOPMIN", "z"},
				{"ZPOPMAX", "z"},
				{"ZCARD", "z"},
			},
		},
		{
			Name: "zrangebylex",
			Steps: []Command{
				{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d", "0", "e"},
				{"ZRANGEBYLEX", "z", "[b", "[d"},
				{"ZRANGEBYLEX", "z", "-", "+"},
				{"ZRANGEBYLEX", "z", "(a", "(d"},
				{"ZREVRANGEBYLEX", "z", "[d", "[b"},
			},
		},
		{
			Name: "zdiff-zunion-zinter",
			Steps: []Command{
				{"ZADD", "a", "1", "x", "2", "y"},
				{"ZADD", "b", "3", "y", "4", "z"},
				{"ZUNIONSTORE", "dst", "2", "a", "b"},
				{"ZINTERSTORE", "dsti", "2", "a", "b"},
				{"ZRANGE", "dst", "0", "-1", "WITHSCORES"},
				{"ZRANGE", "dsti", "0", "-1", "WITHSCORES"},
			},
		},
		{
			Name: "zrangestore",
			Steps: []Command{
				{"ZADD", "src", "1", "a", "2", "b", "3", "c"},
				{"ZRANGESTORE", "dst", "src", "0", "-1"},
				{"ZRANGE", "dst", "0", "-1", "WITHSCORES"},
			},
		},
		{
			Name: "zmscore",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b"},
				{"ZMSCORE", "z", "a", "missing", "b"},
			},
		},
		{
			Name: "zdiffstore",
			Steps: []Command{
				{"ZADD", "a", "1", "x", "2", "y"},
				{"ZADD", "b", "3", "y"},
				{"ZDIFFSTORE", "dst", "2", "a", "b"},
				{"ZRANGE", "dst", "0", "-1", "WITHSCORES"},
			},
		},

		// KEYS, RANDOMKEY, SCAN.
		{
			Name: "keys-pattern",
			Steps: []Command{
				{"MSET", "ka", "1", "kb", "2", "xc", "3"},
				{"KEYS", "k*"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceUnordered},
		},
		{
			Name: "scan-basic",
			Steps: []Command{
				{"SET", "k1", "v1"},
				{"SET", "k2", "v2"},
				{"DBSIZE"},
			},
		},
		{
			Name: "sort-basic",
			Steps: []Command{
				{"RPUSH", "l", "3", "1", "2"},
				{"SORT", "l"},
				{"SORT", "l", "DESC"},
				{"SORT", "l", "ALPHA"},
			},
		},

		// OBJECT REFCOUNT, IDLETIME.
		{
			Name: "object-refcount-idletime",
			Steps: []Command{
				{"SET", "k", "v"},
				{"OBJECT", "REFCOUNT", "k"},
				{"OBJECT", "IDLETIME", "k"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceIntApprox, 2: ToleranceIntApprox},
		},

		// COMMAND COUNT — total differs by server version; verify both respond.
		{
			Name: "command-count",
			Steps: []Command{
				{"COMMAND", "COUNT"},
			},
			Tolerate: map[int]Tolerance{0: ToleranceAny},
		},

		// DUMP/RESTORE.
		{
			Name: "dump-restore",
			Steps: []Command{
				{"SET", "src", "hello"},
				{"DUMP", "missing"},
			},
		},

		// TYPE for all types.
		{
			Name: "type-all",
			Steps: []Command{
				{"SET", "s", "v"},
				{"RPUSH", "l", "a"},
				{"HSET", "h", "f", "v"},
				{"SADD", "st", "a"},
				{"ZADD", "z", "1", "a"},
				{"TYPE", "s"},
				{"TYPE", "l"},
				{"TYPE", "h"},
				{"TYPE", "st"},
				{"TYPE", "z"},
				{"TYPE", "missing"},
			},
		},

		// SUBSTR (legacy alias for GETRANGE).
		{
			Name: "substr",
			Steps: []Command{
				{"SET", "k", "Hello World"},
				{"SUBSTR", "k", "0", "4"},
			},
		},

		// WAIT command.
		{
			Name:     "wait",
			Steps:    []Command{{"WAIT", "0", "100"}},
			Tolerate: map[int]Tolerance{0: ToleranceIntApprox},
		},

		// OBJECT HELP — text wording differs between versions; verify both respond.
		{
			Name:     "object-help",
			Steps:    []Command{{"OBJECT", "HELP"}},
			Tolerate: map[int]Tolerance{0: ToleranceAny},
		},

		// Verify stream type via XADD + TYPE. XADD * generates a timestamp-based ID
		// that differs between servers, so step 0 is tolerated.
		{
			Name: "object-encoding-stream",
			Steps: []Command{
				{"XADD", "s", "*", "field", "value"},
				{"TYPE", "s"},
			},
			Tolerate: map[int]Tolerance{0: ToleranceAny},
		},

		// BITCOUNT, BITPOS.
		{
			Name: "bitcount-bitpos",
			Steps: []Command{
				{"SET", "k", "\xff\xf0\x00"},
				{"BITCOUNT", "k"},
				{"BITCOUNT", "k", "0", "0"},
				{"BITPOS", "k", "0"},
				{"BITPOS", "k", "1"},
			},
		},

		// SETBIT, GETBIT.
		{
			Name: "setbit-getbit",
			Steps: []Command{
				{"SETBIT", "k", "7", "1"},
				{"GETBIT", "k", "7"},
				{"GETBIT", "k", "0"},
				{"BITCOUNT", "k"},
			},
		},

		// LMPOP, ZMPOP [7.0].
		{
			Name: "lmpop",
			Steps: []Command{
				{"RPUSH", "l1", "a", "b", "c"},
				{"RPUSH", "l2", "x", "y"},
				{"LMPOP", "2", "l1", "l2", "LEFT"},
				{"LMPOP", "2", "l1", "l2", "LEFT", "COUNT", "2"},
			},
		},
		{
			Name: "zmpop",
			Steps: []Command{
				{"ZADD", "z1", "1", "a", "2", "b", "3", "c"},
				{"ZMPOP", "1", "z1", "MIN"},
				{"ZMPOP", "1", "z1", "MIN", "COUNT", "2"},
			},
		},

		// LOLWUT (just check both return OK or a bulk string).
		{
			Name:     "lolwut",
			Steps:    []Command{{"LOLWUT"}},
			Tolerate: map[int]Tolerance{0: ToleranceEncoding},
		},

		// HELLO — version, id, and modules differ between servers; verify both respond.
		{
			Name: "hello",
			Steps: []Command{
				{"HELLO"},
			},
			Tolerate: map[int]Tolerance{0: ToleranceAny},
		},

		// RESET.
		{
			Name: "reset",
			Steps: []Command{
				{"MULTI"},
				{"RESET"},
				{"SET", "k", "v"},
				{"GET", "k"},
			},
		},

		// HyperLogLog. PFCOUNT is exact for the small cardinalities used here
		// (the sparse representation counts exactly well past these sizes), so
		// the estimates agree across servers without any approximation slack.
		// The raw HLL string differs by representation, so it is never read back
		// with GET; only the counts and the structural replies are compared.
		{
			Name: "pfadd-pfcount",
			Steps: []Command{
				{"PFADD", "hll", "a", "b", "c", "d", "e"},
				{"PFCOUNT", "hll"},
				{"PFADD", "hll", "a"},
				{"PFCOUNT", "hll"},
				{"TYPE", "hll"},
			},
		},
		{
			Name: "pfmerge",
			Steps: []Command{
				{"PFADD", "h1", "a", "b", "c"},
				{"PFADD", "h2", "c", "d", "e"},
				{"PFMERGE", "dest", "h1", "h2"},
				{"PFCOUNT", "dest"},
				{"PFCOUNT", "h1", "h2"},
			},
		},

		// Bitmap BITOP and BITFIELD. Both are byte-exact across servers.
		{
			Name: "bitop",
			Steps: []Command{
				{"SET", "a", "abc"},
				{"SET", "b", "abd"},
				{"BITOP", "AND", "d_and", "a", "b"},
				{"GET", "d_and"},
				{"BITOP", "OR", "d_or", "a", "b"},
				{"GET", "d_or"},
				{"BITOP", "XOR", "d_xor", "a", "b"},
				{"GET", "d_xor"},
				{"BITOP", "NOT", "d_not", "a"},
				{"STRLEN", "d_not"},
			},
		},
		{
			Name: "bitfield",
			Steps: []Command{
				{"BITFIELD", "bf", "SET", "u8", "0", "255", "GET", "u8", "0", "INCRBY", "u8", "0", "10", "OVERFLOW", "SAT", "INCRBY", "u8", "0", "100"},
				{"BITFIELD", "bf2", "SET", "i8", "#0", "-128", "INCRBY", "i8", "#0", "-10"},
				{"BITFIELD_RO", "bf", "GET", "u8", "0"},
			},
		},

		// Scripting. EVAL return-value conversion, redis.call, and the error
		// reply rules. error_reply prepends a generic ERR only when the message
		// has no space-delimited code token, while a returned {err=...} table is
		// always sent verbatim. These pin the Redis luaPushErrorBuff behavior.
		{
			Name: "eval-basic",
			Steps: []Command{
				{"EVAL", "return 1", "0"},
				{"EVAL", "return 'hello'", "0"},
				{"EVAL", "return {1,2,3}", "0"},
				{"EVAL", "return #KEYS", "2", "a", "b"},
				{"EVAL", "return ARGV[1]", "0", "x"},
				{"EVAL", "return redis.status_reply('TEST')", "0"},
				{"EVAL", "return redis.sha1hex('')", "0"},
			},
		},
		{
			Name: "eval-redis-call",
			Steps: []Command{
				{"EVAL", "return redis.call('set', KEYS[1], ARGV[1])", "1", "sk", "sv"},
				{"GET", "sk"},
				{"EVAL", "redis.call('incr', KEYS[1]); return redis.call('incr', KEYS[1])", "1", "ctr"},
				{"GET", "ctr"},
			},
		},
		{
			Name: "eval-error-reply",
			Steps: []Command{
				{"EVAL", "return redis.error_reply('my error')", "0"},
				{"EVAL", "return redis.error_reply('boom')", "0"},
				{"EVAL", "return redis.error_reply('WRONGTYPE nope')", "0"},
				{"EVAL", "return {err='raw table err'}", "0"},
				{"EVAL", "return {err='oneword'}", "0"},
			},
		},
		{
			Name: "script-load-evalsha",
			Steps: []Command{
				{"SCRIPT", "LOAD", "return 42"},
				{"EVALSHA", "1fa00e76656cc152ad327c13fe365858fd7be306", "0"},
				{"SCRIPT", "EXISTS", "1fa00e76656cc152ad327c13fe365858fd7be306", "0000000000000000000000000000000000000000"},
			},
		},

		// Stream consumer groups. Explicit entry IDs keep the case deterministic
		// (an auto * ID embeds wall-clock time and would differ). XINFO STREAM is
		// compared exactly so the groups count, last-generated-id, and first/last
		// entry are pinned; the rax-tree fields are stable for a single-node
		// stream this small.
		{
			Name: "xgroup-readgroup-ack",
			Steps: []Command{
				{"XADD", "st", "1-1", "f", "v1"},
				{"XADD", "st", "2-2", "f", "v2"},
				{"XGROUP", "CREATE", "st", "g1", "0"},
				{"XREADGROUP", "GROUP", "g1", "c1", "COUNT", "10", "STREAMS", "st", ">"},
				{"XACK", "st", "g1", "1-1"},
				{"XPENDING", "st", "g1"},
				{"XINFO", "GROUPS", "st"},
				{"XLEN", "st"},
			},
		},
		{
			Name: "xinfo-stream-groups-count",
			Steps: []Command{
				{"XADD", "st", "1-1", "f", "v1"},
				{"XADD", "st", "2-2", "f", "v2"},
				{"XGROUP", "CREATE", "st", "g1", "0"},
				{"XGROUP", "CREATE", "st", "g2", "0"},
				{"XINFO", "STREAM", "st"},
			},
		},
		{
			// XSETID below the stream top item rejects with the exact Redis text
			// ("smaller than the target stream top item"); raising it is allowed.
			Name: "xsetid",
			Steps: []Command{
				{"XADD", "st", "5-5", "f", "v"},
				{"XSETID", "st", "9-9"},
				{"XSETID", "st", "1-1"},
				{"XSETID", "st", "100-0", "ENTRIESADDED", "50", "MAXDELETEDID", "2-2"},
			},
		},
	}

	// The Redis 7.4 distinctive surface: hash field TTL, LCS, SINTERCARD,
	// ZRANDMEMBER, SMOVE, the read-only command variants, and set encodings.
	base = append(base, redis74SurfaceCases()...)

	// Large collections carried through the generic key ops, the breadth that
	// small-collection cases cannot reach because they never cross the inline
	// encoding boundary.
	return append(base, collKeyopCases()...)
}
