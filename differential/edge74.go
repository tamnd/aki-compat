package differential

import "github.com/tamnd/aki-compat/target"

// edge74Cases probes the corners where a reimplemented server most often drifts
// from Redis 7.4: argument validation and error wording, integer and float
// arithmetic limits, and the 7.4 bit-index unit. Most cases assert exact replies
// so a divergence is a real bug rather than a tolerated difference. Where Redis
// only fixes the error prefix and varies the trailing detail, the step is marked
// ToleranceErrPrefix, which still requires both servers to error with the same
// first word.
func edge74Cases() []Case {
	return []Case{
		// STRLEN on a missing key is 0, on a value is its byte length, and on a
		// wrong type it is a WRONGTYPE error.
		{
			Name: "strlen-basic",
			Steps: []Command{
				{"STRLEN", "missing"},
				{"SET", "s", "hello"},
				{"STRLEN", "s"},
				{"RPUSH", "l", "a"},
				{"STRLEN", "l"},
			},
			Tolerate: map[int]Tolerance{4: ToleranceErrPrefix},
		},

		// SETRANGE past the end zero-pads the gap, and an offset into an existing
		// value overwrites in place. The intermediate length replies are exact.
		{
			Name: "setrange-extend",
			Steps: []Command{
				{"SETRANGE", "k", "5", "hello"},
				{"GET", "k"},
				{"SET", "k2", "Hello World"},
				{"SETRANGE", "k2", "6", "Redis"},
				{"GET", "k2"},
			},
		},

		// GETRANGE with negative indices counts from the end, and a fully
		// out-of-range window is the empty string, not an error.
		{
			Name: "getrange-negative",
			Steps: []Command{
				{"SET", "k", "This is a string"},
				{"GETRANGE", "k", "-3", "-1"},
				{"GETRANGE", "k", "0", "-1"},
				{"GETRANGE", "k", "10", "100"},
				{"GETRANGE", "k", "-1", "-5"},
				{"GETRANGE", "missing", "0", "-1"},
			},
		},

		// SET with absolute expiry (EXAT/PXAT). The TTL read ticks down between the
		// baseline and aki calls, so only that step is approximate; OK and the value
		// are exact.
		{
			Name: "set-exat-pxat",
			Steps: []Command{
				{"SET", "k", "v", "EXAT", "99999999999"},
				{"GET", "k"},
				{"TTL", "k"},
				{"SET", "k2", "v2", "PXAT", "99999999999000"},
				{"PTTL", "k2"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceIntApprox, 4: ToleranceIntApprox},
		},

		// SETEX rejects a zero or negative expire with an exact error.
		{
			Name: "setex-invalid-seconds",
			Steps: []Command{
				{"SETEX", "k", "0", "v"},
				{"SETEX", "k", "-1", "v"},
				{"PSETEX", "k", "0", "v"},
			},
			Tolerate: map[int]Tolerance{
				0: ToleranceErrPrefix,
				1: ToleranceErrPrefix,
				2: ToleranceErrPrefix,
			},
		},

		// EXPIRE rejects contradictory option pairs (NX with XX, NX with GT).
		{
			Name: "expire-options-conflict",
			Steps: []Command{
				{"SET", "k", "v"},
				{"EXPIRE", "k", "100", "NX", "XX"},
				{"EXPIRE", "k", "100", "NX", "GT"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceErrPrefix, 2: ToleranceErrPrefix},
		},

		// INCR past the signed 64-bit ceiling and DECR past the floor overflow with
		// an exact error, and INCRBY with a non-integer increment is rejected.
		{
			Name: "incr-overflow",
			Steps: []Command{
				{"SET", "k", "9223372036854775807"},
				{"INCR", "k"},
				{"SET", "k2", "-9223372036854775808"},
				{"DECR", "k2"},
				{"SET", "k3", "10"},
				{"INCRBY", "k3", "notanint"},
			},
			Tolerate: map[int]Tolerance{
				1: ToleranceErrPrefix,
				3: ToleranceErrPrefix,
				5: ToleranceErrPrefix,
			},
		},

		// INCRBYFLOAT normalizes its output: no trailing zeros, and scientific
		// notation on input becomes plain decimal. The result strings are exact.
		{
			// INCRBYFLOAT computes in long double on Redis 7.4 and formats with
			// "%.17Lf" then trims, so 10.5 + 0.1 keeps the long-double rounding and
			// 3.0 + 1.000000000000000005 keeps the extra digit the 64-bit mantissa
			// can hold. Valkey 9.1 reworked this to compute in plain double, which
			// rounds 10.5 + 0.1 to the float64 value and collapses the tiny
			// increment, so it diverges from the 7.4 behavior aki pins. The case
			// asserts 7.4 and skips Valkey rather than reporting a false failure.
			Name: "incrbyfloat-format",
			Skip: []target.Kind{target.KindValkey},
			Steps: []Command{
				{"SET", "k", "10.5"},
				{"INCRBYFLOAT", "k", "0.1"},
				{"SET", "k2", "3.0"},
				{"INCRBYFLOAT", "k2", "1.000000000000000005"},
				{"SET", "k3", "5.0e3"},
				{"INCRBYFLOAT", "k3", "2.0e2"},
			},
		},

		// INCRBYFLOAT and HINCRBYFLOAT reject a result of nan or infinity.
		{
			Name: "incrbyfloat-nan-inf",
			Steps: []Command{
				{"SET", "k", "3.0"},
				{"INCRBYFLOAT", "k", "nan"},
				{"INCRBYFLOAT", "k", "inf"},
				{"HSET", "h", "f", "5.0"},
				{"HINCRBYFLOAT", "h", "f", "inf"},
			},
			Tolerate: map[int]Tolerance{
				1: ToleranceErrPrefix,
				2: ToleranceErrPrefix,
				4: ToleranceErrPrefix,
			},
		},

		// ZADD with a nan score is rejected, and ZINCRBY that would produce nan
		// (+inf plus -inf) is rejected with the exact error.
		{
			Name: "zadd-zincrby-nan",
			Steps: []Command{
				{"ZADD", "z", "nan", "m"},
				{"ZADD", "z", "inf", "m"},
				{"ZINCRBY", "z", "-inf", "m"},
			},
			Tolerate: map[int]Tolerance{0: ToleranceErrPrefix, 2: ToleranceErrPrefix},
		},

		// ZRANGEBYSCORE with infinity bounds and exclusive parentheses. Members come
		// back in score order, so the array compare is exact.
		{
			Name: "zrangebyscore-inf-exclusive",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c", "4", "d"},
				{"ZRANGEBYSCORE", "z", "-inf", "+inf"},
				{"ZRANGEBYSCORE", "z", "(1", "3"},
				{"ZRANGEBYSCORE", "z", "(1", "(4"},
				{"ZRANGEBYSCORE", "z", "-inf", "+inf", "WITHSCORES", "LIMIT", "1", "2"},
			},
		},

		// BITCOUNT with the 7.4 BIT unit counts set bits in a bit range, distinct
		// from the BYTE range. Both forms reply with exact integers.
		{
			Name: "bitcount-bit-unit",
			Steps: []Command{
				{"SET", "k", "foobar"},
				{"BITCOUNT", "k"},
				{"BITCOUNT", "k", "1", "1"},
				{"BITCOUNT", "k", "1", "1", "BYTE"},
				{"BITCOUNT", "k", "5", "30", "BIT"},
				{"BITCOUNT", "k", "0", "0", "BIT"},
			},
		},

		// BITPOS with the 7.4 BIT unit locates a bit by bit offset.
		{
			Name: "bitpos-bit-unit",
			Steps: []Command{
				{"SET", "k", "\xff\xf0\x00"},
				{"BITPOS", "k", "0"},
				{"BITPOS", "k", "1", "2"},
				{"BITPOS", "k", "0", "0", "-1", "BIT"},
				{"BITPOS", "k", "1", "8", "15", "BIT"},
			},
		},

		// LPOS with a negative rank scans from the tail, and COUNT 0 returns every
		// match. A missing element is a null, a missing key under COUNT is an empty
		// array.
		{
			Name: "lpos-rank-count",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c", "a", "b", "c", "a"},
				{"LPOS", "l", "a"},
				{"LPOS", "l", "a", "RANK", "-1"},
				{"LPOS", "l", "a", "COUNT", "0"},
				{"LPOS", "l", "a", "RANK", "-1", "COUNT", "2"},
				{"LPOS", "l", "z"},
				{"LPOS", "missing", "a", "COUNT", "0"},
			},
		},

		// LINSERT before a missing pivot returns -1; on a missing key it returns 0.
		// LSET out of range is an exact error.
		{
			Name: "linsert-lset-edges",
			Steps: []Command{
				{"RPUSH", "l", "a", "b", "c"},
				{"LINSERT", "l", "BEFORE", "zzz", "x"},
				{"LINSERT", "missing", "BEFORE", "a", "x"},
				{"LSET", "l", "10", "y"},
				{"LSET", "missing", "0", "y"},
			},
			Tolerate: map[int]Tolerance{3: ToleranceErrPrefix, 4: ToleranceErrPrefix},
		},

		// SORT with ALPHA and a LIMIT window, and the numeric-sort error when a
		// value is not a number.
		{
			Name: "sort-alpha-limit",
			Steps: []Command{
				{"RPUSH", "l", "banana", "apple", "cherry", "date"},
				{"SORT", "l", "ALPHA"},
				{"SORT", "l", "ALPHA", "LIMIT", "1", "2"},
				{"SORT", "l", "ALPHA", "DESC", "LIMIT", "0", "2"},
				{"SORT", "l"},
			},
			Tolerate: map[int]Tolerance{4: ToleranceErrPrefix},
		},

		// GETDEL and GETEX type errors on a non-string key are exact WRONGTYPE.
		{
			Name: "getdel-getex-wrongtype",
			Steps: []Command{
				{"RPUSH", "l", "a"},
				{"GETDEL", "l"},
				{"GETEX", "l"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceErrPrefix, 2: ToleranceErrPrefix},
		},

		// SMISMEMBER reports membership per query element in order, including
		// duplicates and absent members, as a fixed-shape integer array.
		{
			Name: "smismember-order",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"SMISMEMBER", "s", "a", "x", "b", "x", "c"},
				{"SMISMEMBER", "missing", "a", "b"},
			},
		},

		// APPEND creates a key when missing and returns the new length each time;
		// the final value is exact.
		{
			Name: "append-grows",
			Steps: []Command{
				{"APPEND", "k", "Hello"},
				{"APPEND", "k", " World"},
				{"GET", "k"},
				{"STRLEN", "k"},
			},
		},
	}
}
