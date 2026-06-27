package differential

// scan74Cases covers the cursor scan family (SCAN, SSCAN, HSCAN, ZSCAN, the 7.4
// HSCAN NOVALUES form) and a cluster of single-key commands the base table never
// reached: RPOPLPUSH, the conditional pushes LPUSHX and RPUSHX, TOUCH, and the
// random-member readers HRANDFIELD WITHVALUES and SRANDMEMBER with a count
// argument.
//
// The scan replies are a two element [cursor, elements] array whose cursor value
// and element order are both implementation specific. Every scan step here uses a
// COUNT large enough that the whole collection comes back in one call, so the
// cursor is "0" on both servers, and tolerates the reply with ToleranceScan,
// which checks the cursor matches and compares the element list unordered.
// Everything else is asserted exactly: a divergence in a rotated value, a
// conditional push count, a touched-key count, or a WRONGTYPE error is a real
// compatibility break.
func scan74Cases() []Case {
	return []Case{
		// SCAN walks the keyspace. With a high COUNT a small keyspace returns in one
		// call with cursor "0" and every key present. MATCH filters by glob.
		{
			Name: "scan-keyspace",
			Steps: []Command{
				{"MSET", "k:1", "a", "k:2", "b", "k:3", "c", "other", "d"},
				{"SCAN", "0", "COUNT", "1000"},
				{"SCAN", "0", "MATCH", "k:*", "COUNT", "1000"},
				{"SCAN", "0", "MATCH", "nope:*", "COUNT", "1000"},
				{"SCAN", "0", "TYPE", "string", "COUNT", "1000"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceScan, 2: ToleranceScan, 3: ToleranceScan, 4: ToleranceScan},
		},
		// SSCAN walks a set's members. The reply order is unspecified like SMEMBERS,
		// so the scan tolerance compares the member list unordered.
		{
			Name: "sscan-members",
			Steps: []Command{
				{"SADD", "s", "alpha", "beta", "gamma", "delta"},
				{"SSCAN", "s", "0", "COUNT", "1000"},
				{"SSCAN", "s", "0", "MATCH", "*a", "COUNT", "1000"},
				{"SSCAN", "missing", "0", "COUNT", "1000"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceScan, 2: ToleranceScan, 3: ToleranceScan},
		},
		// HSCAN walks a hash. The default reply interleaves field and value; the 7.4
		// NOVALUES option returns fields only. Both forms are scan replies whose pair
		// or field order is unspecified.
		{
			Name: "hscan-fields-and-novalues",
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2", "f3", "v3"},
				{"HSCAN", "h", "0", "COUNT", "1000"},
				{"HSCAN", "h", "0", "NOVALUES", "COUNT", "1000"},
				{"HSCAN", "h", "0", "MATCH", "f1", "COUNT", "1000"},
				{"HSCAN", "h", "0", "MATCH", "f*", "NOVALUES", "COUNT", "1000"},
				{"HSCAN", "missing", "0", "COUNT", "1000"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceScan, 2: ToleranceScan, 3: ToleranceScan, 4: ToleranceScan, 5: ToleranceScan},
		},
		// ZSCAN walks a sorted set, interleaving member and score. The pair order is
		// unspecified, so it is a scan reply too.
		{
			Name: "zscan-members",
			Steps: []Command{
				{"ZADD", "z", "1", "one", "2", "two", "3", "three"},
				{"ZSCAN", "z", "0", "COUNT", "1000"},
				{"ZSCAN", "z", "0", "MATCH", "t*", "COUNT", "1000"},
				{"ZSCAN", "missing", "0", "COUNT", "1000"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceScan, 2: ToleranceScan, 3: ToleranceScan},
		},
		// RPOPLPUSH pops the tail of the source and pushes it to the head of the
		// destination, returning the moved element. Rotating onto the same key cycles
		// the list. The reply at each step is deterministic, so this is exact.
		{
			Name: "rpoplpush-move-and-rotate",
			Steps: []Command{
				{"RPUSH", "src", "a", "b", "c"},
				{"RPOPLPUSH", "src", "dst"},
				{"LRANGE", "src", "0", "-1"},
				{"LRANGE", "dst", "0", "-1"},
				{"RPOPLPUSH", "self", "self"}, // missing key returns nil, no-op
				{"RPUSH", "rot", "1", "2", "3"},
				{"RPOPLPUSH", "rot", "rot"},
				{"LRANGE", "rot", "0", "-1"},
				{"SET", "str", "x"},
				{"RPOPLPUSH", "str", "dst"}, // WRONGTYPE
			},
			Tolerate: map[int]Tolerance{9: ToleranceErrPrefix},
		},
		// LPUSHX and RPUSHX push only when the key already holds a list. A missing key
		// returns 0 and creates nothing; an existing list grows and returns the new
		// length. A wrong type is a WRONGTYPE error.
		{
			Name: "lpushx-rpushx",
			Steps: []Command{
				{"LPUSHX", "missing", "v"},
				{"RPUSHX", "missing", "v"},
				{"EXISTS", "missing"},
				{"RPUSH", "l", "b"},
				{"LPUSHX", "l", "a"},
				{"RPUSHX", "l", "c"},
				{"LRANGE", "l", "0", "-1"},
				{"LPUSHX", "l", "a1", "a2"}, // multi-value form
				{"LRANGE", "l", "0", "-1"},
				{"SET", "str", "x"},
				{"LPUSHX", "str", "v"}, // WRONGTYPE
			},
			Tolerate: map[int]Tolerance{10: ToleranceErrPrefix},
		},
		// TOUCH returns the count of keys that exist among its arguments, counting a
		// key once per appearance. It does not create keys.
		{
			Name: "touch-counts-existing",
			Steps: []Command{
				{"MSET", "t1", "a", "t2", "b"},
				{"TOUCH", "t1"},
				{"TOUCH", "t1", "t2"},
				{"TOUCH", "t1", "missing", "t2"},
				{"TOUCH", "missing1", "missing2"},
				{"TOUCH", "t1", "t1"}, // same key twice counts twice
			},
		},
		// HRANDFIELD with a positive count returns distinct fields; WITHVALUES pairs
		// each with its value. A count larger than the hash returns the whole hash.
		// The returned field set is unspecified in order, so the distinct replies
		// tolerate ordering. A positive count at or above the hash size returns every
		// field, so the contents are determined and the unordered compare is exact on
		// the multiset. The missing-key replies are an empty array and a nil, both
		// deterministic and asserted exactly.
		{
			Name: "hrandfield-withvalues",
			Steps: []Command{
				{"HSET", "h", "f1", "v1", "f2", "v2", "f3", "v3"},
				{"HRANDFIELD", "h", "3"},
				{"HRANDFIELD", "h", "3", "WITHVALUES"},
				{"HRANDFIELD", "h", "10"},               // capped at the 3 fields, all distinct
				{"HRANDFIELD", "h", "10", "WITHVALUES"}, // every field with its value
				{"HRANDFIELD", "missing", "3"},
				{"HRANDFIELD", "missing"},
			},
			Tolerate: map[int]Tolerance{
				1: ToleranceUnordered, 2: ToleranceUnordered, 3: ToleranceUnordered, 4: ToleranceUnordered,
			},
		},
		// SRANDMEMBER with a positive count returns distinct members capped at the set
		// size; with a negative count it allows repeats and can exceed the size. The
		// distinct forms are order-unspecified, so they tolerate ordering.
		{
			Name: "srandmember-counts",
			Steps: []Command{
				{"SADD", "s", "a", "b", "c"},
				{"SRANDMEMBER", "s", "3"},
				{"SRANDMEMBER", "s", "10"}, // capped at 3, all distinct
				{"SRANDMEMBER", "s", "0"},  // empty array
				{"SRANDMEMBER", "missing", "3"},
				{"SRANDMEMBER", "missing"},
			},
			Tolerate: map[int]Tolerance{1: ToleranceUnordered, 2: ToleranceUnordered},
		},
	}
}
