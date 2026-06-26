package differential

// Redis 7.4 distinctive surface. These cases target commands and options that the
// core table did not reach: the hash field TTL family that is the 7.4 headline
// feature (HEXPIRE, HTTL, HPERSIST, HEXPIREAT, HEXPIRETIME), plus LCS, SINTERCARD,
// ZRANDMEMBER, SMOVE, the read-only SORT_RO and BITFIELD_RO variants, set encoding
// transitions, and the ZADD GT/LT guards.
//
// HGETEX and HGETDEL are deliberately absent: they were added in Redis 8.0, so a
// 7.4 baseline answers them with "unknown command". aki implements them as a
// forward-compatible superset, which a 7.4-surface suite has no business asserting
// either way.
//
// Every case is built so its reply is deterministic across two live servers. The
// hash TTL cases never compare a live countdown value: they assert the status
// codes (1 set, 2 deleted, 0 condition unmet, -2 no field), the no-ttl sentinel
// (-1), and the absolute expire time set by HEXPIREAT (which both servers echo
// back unchanged). Absolute expiries stay under the ebuckets ceiling (2^46-1 ms),
// the largest deadline Redis 7.4 accepts for a hash field. Random-order replies
// use ToleranceUnordered.
func redis74SurfaceCases() []Case {
	return []Case{
		// Hash field TTL: status codes are deterministic. HEXPIRE returns 1 when the
		// TTL is set, -2 for a missing field. A persisted field reports -1 from HTTL.
		{
			Name: "hexpire-httl-hpersist",
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2"},
				{"HEXPIRE", "h", "100", "FIELDS", "1", "f1"},
				{"HEXPIRE", "h", "100", "FIELDS", "1", "nofield"},
				{"HTTL", "h", "FIELDS", "1", "f2"},
				{"HTTL", "h", "FIELDS", "1", "nofield"},
				{"HPERSIST", "h", "FIELDS", "1", "f1"},
				{"HTTL", "h", "FIELDS", "1", "f1"},
				{"HPERSIST", "h", "FIELDS", "1", "f2"},
			},
		},
		// HEXPIREAT sets an absolute second-precision expiry. HEXPIRETIME echoes that
		// exact second and HPEXPIRETIME echoes it in milliseconds, so both are
		// deterministic across servers.
		{
			Name: "hexpireat-hexpiretime",
			Steps: []Command{
				{"HSET", "h", "f1", "v1"},
				{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "f1"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "f1"},
				{"HPEXPIRETIME", "h", "FIELDS", "1", "f1"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "nofield"},
			},
		},
		// The NX, XX, GT and LT guards on HEXPIRE resolve to deterministic status
		// codes given a fixed sequence of absolute expiries, all under the ebuckets
		// ceiling so Redis 7.4 accepts every one.
		{
			Name: "hexpire-nx-xx-gt-lt",
			Steps: []Command{
				{"HSET", "h", "f1", "v1"},
				{"HEXPIREAT", "h", "9999999999", "NX", "FIELDS", "1", "f1"},
				{"HEXPIREAT", "h", "9999999998", "NX", "FIELDS", "1", "f1"},
				{"HEXPIREAT", "h", "9999999998", "XX", "FIELDS", "1", "f1"},
				{"HEXPIREAT", "h", "8888888888", "GT", "FIELDS", "1", "f1"},
				{"HEXPIREAT", "h", "9999999999", "GT", "FIELDS", "1", "f1"},
				{"HEXPIREAT", "h", "7777777777", "LT", "FIELDS", "1", "f1"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "f1"},
			},
		},
		// The ebuckets ceiling. Redis 7.4 packs a hash field deadline into 46 bits,
		// so an absolute expiry past 2^46-1 ms is rejected with the invalid-expire
		// error. 99999999999 s is well past it. Both servers must refuse it
		// identically (aki gained this cap to match). The string EXPIRE family has
		// no such ceiling, which is why this is asserted only on the hash setter.
		{
			Name: "hexpireat-over-ebuckets-max",
			Steps: []Command{
				{"HSET", "h", "f1", "v1"},
				{"HEXPIREAT", "h", "99999999999", "FIELDS", "1", "f1"},
				{"HTTL", "h", "FIELDS", "1", "f1"},
			},
		},
		// HEXPIRE with a past or zero TTL deletes the field and reports 2; the field
		// is then gone and an empty hash is removed like Redis removes it.
		{
			Name: "hexpire-past-deletes-field",
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2"},
				{"HEXPIRE", "h", "0", "FIELDS", "1", "f1"},
				{"HEXISTS", "h", "f1"},
				{"HGET", "h", "f2"},
				{"HEXPIREAT", "h", "1", "FIELDS", "1", "f2"},
				{"EXISTS", "h"},
			},
		},
		// LCS, the longest common subsequence command. The string, the length, and
		// the IDX match structure are all deterministic for fixed inputs.
		{
			Name: "lcs",
			Steps: []Command{
				{"MSET", "key1", "ohmytext", "key2", "mynewtext"},
				{"LCS", "key1", "key2"},
				{"LCS", "key1", "key2", "LEN"},
				{"LCS", "key1", "key2", "IDX"},
				{"LCS", "key1", "key2", "IDX", "MINMATCHLEN", "4"},
				{"LCS", "key1", "key2", "IDX", "WITHMATCHLEN"},
			},
			Proto: 3,
		},

		// SINTERCARD with and without LIMIT.
		{
			Name: "sintercard",
			Steps: []Command{
				{"SADD", "s1", "a", "b", "c", "d"},
				{"SADD", "s2", "b", "c", "d", "e"},
				{"SINTERCARD", "2", "s1", "s2"},
				{"SINTERCARD", "2", "s1", "s2", "LIMIT", "2"},
				{"SINTERCARD", "2", "s1", "s2", "LIMIT", "0"},
			},
		},

		// ZRANDMEMBER. A positive count returns distinct members in unspecified
		// order, a count of zero returns an empty array, and a count larger than the
		// cardinality returns every member once. Single-member random replies are
		// left out because they differ between servers by design.
		{
			Name: "zrandmember-count",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c"},
				{"ZRANDMEMBER", "z", "3"},
				{"ZRANDMEMBER", "z", "0"},
				{"ZRANDMEMBER", "z", "10"},
				{"ZRANDMEMBER", "missing", "3"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceUnordered, 3: ToleranceUnordered},
		},

		// SMOVE between sets, including the no-op when the member is absent.
		{
			Name: "smove",
			Steps: []Command{
				{"SADD", "src", "a", "b", "c"},
				{"SADD", "dst", "x"},
				{"SMOVE", "src", "dst", "b"},
				{"SMOVE", "src", "dst", "nope"},
				{"SISMEMBER", "dst", "b"},
				{"SISMEMBER", "src", "b"},
				{"SMEMBERS", "src"},
				{"SMEMBERS", "dst"},
			},
			Tolerate: map[int]Tolerance{6: ToleranceUnordered, 7: ToleranceUnordered},
		},

		// ZADD GT and LT only move a score in the allowed direction, and CH reports
		// how many scores actually changed.
		{
			Name: "zadd-gt-lt-ch",
			Steps: []Command{
				{"ZADD", "z", "5", "m"},
				{"ZADD", "z", "GT", "CH", "3", "m"},
				{"ZSCORE", "z", "m"},
				{"ZADD", "z", "GT", "CH", "9", "m"},
				{"ZSCORE", "z", "m"},
				{"ZADD", "z", "LT", "CH", "12", "m"},
				{"ZSCORE", "z", "m"},
				{"ZADD", "z", "LT", "CH", "2", "m"},
				{"ZSCORE", "z", "m"},
			},
		},

		// SORT_RO, the read-only SORT that a replica accepts.
		{
			Name: "sort-ro",
			Steps: []Command{
				{"RPUSH", "l", "3", "1", "2"},
				{"SORT_RO", "l"},
				{"SORT_RO", "l", "DESC"},
				{"SORT_RO", "l", "LIMIT", "0", "2"},
			},
		},

		// BITFIELD_RO, the read-only BITFIELD that only allows GET.
		{
			Name: "bitfield-ro",
			Steps: []Command{
				{"SET", "bf", "\x01\x02\x03"},
				{"BITFIELD_RO", "bf", "GET", "u8", "0"},
				{"BITFIELD_RO", "bf", "GET", "u8", "8", "GET", "u8", "16"},
			},
		},

		// Set OBJECT ENCODING transitions. A small all-integer set is an intset, a
		// small set with a non-integer member is a listpack (7.2 and later), and a
		// set pushed past the listpack entry limit promotes to a hashtable.
		{
			Name: "object-encoding-set-listpack",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"OBJECT", "ENCODING", "s"},
			},
		},
		{
			Name: "object-encoding-set-intset-to-hashtable",
			Steps: []Command{
				{"SADD", "s", "1", "2", "3"},
				{"OBJECT", "ENCODING", "s"},
				{"SADD", "s", "notanumber"},
				{"OBJECT", "ENCODING", "s"},
			},
		},
	}
}
