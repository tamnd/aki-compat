package differential

import (
	"fmt"
	"strconv"
)

// collKeyopCases exercises large collections carried through the generic key
// operations: EXPIRE, PERSIST, RENAME, RENAMENX, COPY, COPY REPLACE, and MOVE.
//
// The point of "large" is the encoding boundary. A small hash or set is stored
// inline on both Redis (listpack) and aki (inline blob); a large one crosses
// into the heavier form (Redis hashtable, aki a metadata row pointing at an
// element sub-tree). The generic key ops carry a value verbatim from one name or
// database to another, and a server that mishandles the heavy form (for example
// by reading back the metadata counters as if they were the value) corrupts the
// collection in a way no small-collection case can reach. These cases build a
// collection past that boundary, run a key op, then read the collection back and
// assert it is intact, comparing every step against Redis.
//
// The element count is chosen to clear the boundary on both servers with room to
// spare; the exact value does not matter as long as it is well past any inline
// threshold.
func collKeyopCases() []Case {
	const n = 256

	return []Case{
		{
			// EXPIRE then PERSIST round-trips the value through the TTL machinery.
			// The hash must read back intact after each, and the TTL replies are
			// exact except the live countdown, which ticks between the two servers.
			Name: "coll-hash-survives-expire-persist",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"EXPIRE", "h", "1000"},
					{"HLEN", "h"},
					{"HGET", "h", "f00100"},
					{"TTL", "h"},
					{"PERSIST", "h"},
					{"HLEN", "h"},
					{"HGET", "h", "f00100"},
					{"TTL", "h"},
				},
			),
			Tolerate: map[int]Tolerance{
				// Step index of the first TTL: hset is step 0, so EXPIRE is 1 and the
				// first TTL is 3. It counts down by one between servers.
				4: ToleranceIntApprox,
			},
		},
		{
			Name: "coll-hash-survives-rename",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"RENAME", "h", "h2"},
					{"EXISTS", "h"},
					{"HLEN", "h2"},
					{"HGET", "h2", "f00100"},
					{"HGET", "h2", "f00255"},
				},
			),
		},
		{
			Name: "coll-hash-survives-renamenx",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"RENAMENX", "h", "h2"},
					{"HLEN", "h2"},
					{"HGET", "h2", "f00100"},
				},
			),
		},
		{
			Name: "coll-hash-survives-copy",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"COPY", "h", "h2"},
					{"HLEN", "h2"},
					{"HGET", "h2", "f00100"},
					// The source is untouched by a copy.
					{"HLEN", "h"},
					{"HGET", "h", "f00200"},
				},
			),
		},
		{
			// COPY without REPLACE onto an existing key replies 0 and leaves the
			// destination alone; with REPLACE it overwrites it with the collection.
			Name: "coll-hash-survives-copy-replace",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"SET", "h2", "occupied"},
					{"COPY", "h", "h2"},
					{"GET", "h2"},
					{"COPY", "h", "h2", "REPLACE"},
					{"HLEN", "h2"},
					{"HGET", "h2", "f00100"},
				},
			),
		},
		{
			// MOVE carries the collection to another database. The case reads it back
			// under SELECT 1, then flushes db 1 and returns to db 0 so it leaves no
			// residue (the runner's FLUSHDB only clears the current database).
			Name: "coll-hash-survives-move",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"MOVE", "h", "1"},
					{"EXISTS", "h"},
					{"SELECT", "1"},
					{"HLEN", "h"},
					{"HGET", "h", "f00100"},
					{"FLUSHDB"},
					{"SELECT", "0"},
				},
			),
		},

		// The other map and ordered types through RENAME, the most common carry.
		{
			Name: "coll-set-survives-rename",
			Steps: join(
				[]Command{saddBig("s", n)},
				[]Command{
					{"RENAME", "s", "s2"},
					{"SCARD", "s2"},
					{"SISMEMBER", "s2", "m00100"},
					{"SISMEMBER", "s2", "m99999"},
				},
			),
		},
		{
			Name: "coll-zset-survives-rename",
			Steps: join(
				[]Command{zaddBig("z", n)},
				[]Command{
					{"RENAME", "z", "z2"},
					{"ZCARD", "z2"},
					{"ZSCORE", "z2", "m00100"},
					{"ZRANK", "z2", "m00100"},
				},
			),
		},
		{
			Name: "coll-list-survives-rename",
			Steps: join(
				[]Command{rpushBig("l", n)},
				[]Command{
					{"RENAME", "l", "l2"},
					{"LLEN", "l2"},
					{"LINDEX", "l2", "100"},
					{"LINDEX", "l2", "-1"},
				},
			),
		},
		{
			Name: "coll-zset-survives-copy",
			Steps: join(
				[]Command{zaddBig("z", n)},
				[]Command{
					{"COPY", "z", "z2"},
					{"ZCARD", "z2"},
					{"ZSCORE", "z2", "m00200"},
				},
			),
		},
	}
}

// hsetBig builds one HSET that writes n fields fNNNNN -> vNNNNN, enough to push
// the hash past the inline encoding boundary on both servers.
func hsetBig(key string, n int) Command {
	c := Command{"HSET", key}
	for i := 0; i < n; i++ {
		c = append(c, fmt.Sprintf("f%05d", i), fmt.Sprintf("v%05d", i))
	}
	return c
}

// saddBig builds one SADD that adds n members mNNNNN.
func saddBig(key string, n int) Command {
	c := Command{"SADD", key}
	for i := 0; i < n; i++ {
		c = append(c, fmt.Sprintf("m%05d", i))
	}
	return c
}

// zaddBig builds one ZADD that adds n members mNNNNN, each with score equal to
// its index so ZSCORE and ZRANK are exact integers both servers print the same.
func zaddBig(key string, n int) Command {
	c := Command{"ZADD", key}
	for i := 0; i < n; i++ {
		c = append(c, strconv.Itoa(i), fmt.Sprintf("m%05d", i))
	}
	return c
}

// rpushBig builds one RPUSH that appends n elements eNNNNN in order.
func rpushBig(key string, n int) Command {
	c := Command{"RPUSH", key}
	for i := 0; i < n; i++ {
		c = append(c, fmt.Sprintf("e%05d", i))
	}
	return c
}

// join concatenates command groups into one Steps slice.
func join(groups ...[]Command) []Command {
	var out []Command
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}
