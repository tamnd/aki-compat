package differential

// setZsetOpCases covers the set algebra commands and the sorted-set range and
// aggregate commands that the base table only touched through their STORE
// variants. These are the commands a reimplementation tends to get almost right:
// the member math is easy, but the reply shape (a plain array for the RESP2 set
// commands, a score-ordered array for the zset commands), the WITHSCORES
// interleave, the aggregate mode, and the rank and lex bound parsing each hide a
// corner.
//
// The set commands reply as an unordered RESP2 array, so those steps tolerate
// ordering. Everything else asserts exact replies: a divergence in a score order,
// an aggregate total, or a removed count is a real compatibility break.
func setZsetOpCases() []Case {
	return []Case{
		// SINTER, SUNION and SDIFF return members, not a stored count. The reply is a
		// plain array on RESP2 with unspecified order.
		{
			Name: "sinter-sunion-sdiff",
			Steps: []Command{
				{"SADD", "a", "1", "2", "3", "4"},
				{"SADD", "b", "3", "4", "5", "6"},
				{"SINTER", "a", "b"},
				{"SUNION", "a", "b"},
				{"SDIFF", "a", "b"},
				{"SDIFF", "b", "a"},
				{"SINTER", "a", "missing"},
				{"SUNION", "a", "missing"},
			},
			Tolerate: map[int]Tolerance{2: ToleranceUnordered, 3: ToleranceUnordered, 4: ToleranceUnordered, 5: ToleranceUnordered, 6: ToleranceUnordered, 7: ToleranceUnordered},
		},
		// SINTERSTORE writes the result and replies with its cardinality. The stored
		// set is then read back to confirm the members landed.
		{
			Name: "sinterstore-then-read",
			Steps: []Command{
				{"SADD", "a", "x", "y", "z"},
				{"SADD", "b", "y", "z", "w"},
				{"SINTERSTORE", "dest", "a", "b"},
				{"SMEMBERS", "dest"},
				{"SCARD", "dest"},
				{"TYPE", "dest"},
			},
			Tolerate: map[int]Tolerance{3: ToleranceUnordered},
		},
		// SINTERSTORE with an empty intersection deletes the destination key rather
		// than leaving an empty set, matching Redis.
		{
			Name: "sinterstore-empty-deletes-dest",
			Steps: []Command{
				{"SET", "dest", "placeholder"},
				{"SADD", "a", "1", "2"},
				{"SADD", "b", "3", "4"},
				{"SINTERSTORE", "dest", "a", "b"},
				{"EXISTS", "dest"},
			},
		},
		// SINTERCARD counts the intersection without building it, and LIMIT 0 means
		// no limit while a positive LIMIT caps the count.
		{
			Name: "sintercard-limit",
			Steps: []Command{
				{"SADD", "a", "1", "2", "3", "4", "5"},
				{"SADD", "b", "2", "3", "4", "5", "6"},
				{"SINTERCARD", "2", "a", "b"},
				{"SINTERCARD", "2", "a", "b", "LIMIT", "0"},
				{"SINTERCARD", "2", "a", "b", "LIMIT", "2"},
			},
		},

		// ZDIFF, ZINTER and ZUNION return members ordered by the resulting score, so
		// unlike the set commands the order is deterministic and compared exactly.
		// WITHSCORES interleaves the score after each member.
		{
			Name: "zunion-zinter-zdiff",
			Steps: []Command{
				{"ZADD", "z1", "1", "a", "2", "b", "3", "c"},
				{"ZADD", "z2", "10", "b", "20", "c", "30", "d"},
				{"ZUNION", "2", "z1", "z2"},
				{"ZUNION", "2", "z1", "z2", "WITHSCORES"},
				{"ZINTER", "2", "z1", "z2", "WITHSCORES"},
				{"ZDIFF", "2", "z1", "z2", "WITHSCORES"},
			},
		},
		// The aggregate mode changes how shared members combine: SUM is the default,
		// MIN and MAX take the extreme, and WEIGHTS scales each input first.
		{
			Name: "zunion-aggregate-weights",
			Steps: []Command{
				{"ZADD", "z1", "1", "x", "2", "y"},
				{"ZADD", "z2", "10", "x", "20", "y"},
				{"ZUNION", "2", "z1", "z2", "AGGREGATE", "MIN", "WITHSCORES"},
				{"ZUNION", "2", "z1", "z2", "AGGREGATE", "MAX", "WITHSCORES"},
				{"ZUNION", "2", "z1", "z2", "WEIGHTS", "2", "3", "WITHSCORES"},
				{"ZINTERCARD", "2", "z1", "z2"},
				{"ZINTERCARD", "2", "z1", "z2", "LIMIT", "1"},
			},
		},
		// ZCOUNT counts by score range, including the exclusive bound and the
		// infinities, and is compared exactly as an integer.
		{
			Name: "zcount-score-range",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c", "4", "d", "5", "e"},
				{"ZCOUNT", "z", "-inf", "+inf"},
				{"ZCOUNT", "z", "2", "4"},
				{"ZCOUNT", "z", "(2", "4"},
				{"ZCOUNT", "z", "(2", "(4"},
				{"ZCOUNT", "z", "5", "1"},
			},
		},
		// ZLEXCOUNT counts by lexicographic range, which only makes sense when every
		// member shares one score. The bracket, paren and infinity forms all parse.
		{
			Name: "zlexcount-lex-range",
			Steps: []Command{
				{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d", "0", "e"},
				{"ZLEXCOUNT", "z", "-", "+"},
				{"ZLEXCOUNT", "z", "[b", "[d"},
				{"ZLEXCOUNT", "z", "(b", "(d"},
				{"ZLEXCOUNT", "z", "[b", "(d"},
			},
		},
		// ZREVRANGE and ZREVRANK walk the set from the top score down. ZREVRANK on a
		// missing member is a nil reply, not zero.
		{
			Name: "zrevrange-zrevrank",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c"},
				{"ZREVRANGE", "z", "0", "-1"},
				{"ZREVRANGE", "z", "0", "-1", "WITHSCORES"},
				{"ZREVRANGE", "z", "0", "0"},
				{"ZREVRANK", "z", "a"},
				{"ZREVRANK", "z", "c"},
				{"ZREVRANK", "z", "missing"},
			},
		},
		// ZREMRANGEBYRANK removes by zero-based rank range and replies with the count
		// removed; the survivors are read back to confirm the right slice went.
		{
			Name: "zremrangebyrank",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c", "4", "d", "5", "e"},
				{"ZREMRANGEBYRANK", "z", "0", "1"},
				{"ZRANGE", "z", "0", "-1", "WITHSCORES"},
				{"ZREMRANGEBYRANK", "z", "-1", "-1"},
				{"ZRANGE", "z", "0", "-1"},
			},
		},
		// ZREMRANGEBYSCORE removes by score range, honoring the exclusive bound.
		{
			Name: "zremrangebyscore",
			Steps: []Command{
				{"ZADD", "z", "1", "a", "2", "b", "3", "c", "4", "d", "5", "e"},
				{"ZREMRANGEBYSCORE", "z", "2", "4"},
				{"ZRANGE", "z", "0", "-1", "WITHSCORES"},
				{"ZREMRANGEBYSCORE", "z", "(1", "+inf"},
				{"ZRANGE", "z", "0", "-1"},
			},
		},
		// ZREMRANGEBYLEX removes by lexicographic range over an equal-score set.
		{
			Name: "zremrangebylex",
			Steps: []Command{
				{"ZADD", "z", "0", "a", "0", "b", "0", "c", "0", "d", "0", "e"},
				{"ZREMRANGEBYLEX", "z", "[b", "[d"},
				{"ZRANGE", "z", "0", "-1"},
				{"ZREMRANGEBYLEX", "z", "-", "+"},
				{"EXISTS", "z"},
			},
		},
	}
}
