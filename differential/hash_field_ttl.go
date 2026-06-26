package differential

// hashFieldTTLCases exercises the Redis 7.4 hash-field expiry family: HEXPIRE,
// HPEXPIRE, HEXPIREAT, HPEXPIREAT, HPERSIST, HTTL, HPTTL, HEXPIRETIME,
// HPEXPIRETIME, HGETEX and HGETDEL.
//
// Two things make these cases worth their own file. First, every assertion is
// timing independent: the live countdown that HTTL/HPTTL report would tick
// between two servers, so these cases never read a live remaining time. They
// read flag codes (1, 0, -1, -2, 2), or they pin an absolute deadline with
// HEXPIREAT and read it straight back with HEXPIRETIME, which is identical on
// every server because the deadline is an absolute Unix time the case chose. The
// NX/XX/GT/LT cases use deadlines that differ by tens of thousands of seconds, a
// gap no clock skew between two local servers can cross, so the flag result is
// deterministic.
//
// Second, each flow runs twice, once on a small hash that stays in the inline
// (listpack) encoding and once on a hash pushed past the hashtable threshold.
// The two encodings are different code paths: the inline hash carries its field
// TTLs in the single blob, the large hash carries one TTL per element row in the
// btree-backed sub-tree. A server can implement the family correctly for one
// form and not the other, so the large-hash variant is not redundant. It is the
// variant that caught aki decoding the metadata cell as if it were an inline
// hash blob.
func hashFieldTTLCases() []Case {
	const n = 256

	return []Case{
		// HEXPIREAT pins an absolute deadline; HEXPIRETIME/HPEXPIRETIME read it
		// back exactly, so this is exact on every server. A field with no TTL
		// reads -1, a missing field -2, a missing key -2.
		{
			Name: "hexpiretime-absolute-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2"},
				{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "a"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "a"},
				{"HPEXPIRETIME", "h", "FIELDS", "1", "a"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "b"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "missing"},
				{"HEXPIRETIME", "nokey", "FIELDS", "1", "a"},
				{"HTTL", "nokey", "FIELDS", "1", "a"},
			},
		},
		{
			Name: "hexpiretime-absolute-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "f00100"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "f00100"},
					{"HPEXPIRETIME", "h", "FIELDS", "1", "f00100"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "f00101"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "missing"},
					{"HTTL", "h", "FIELDS", "1", "f00101"},
				},
			),
		},

		// NX/XX/GT/LT flag results. The deadlines are far apart so the comparison
		// outcome never depends on the millisecond the command lands on.
		{
			Name: "hexpire-flags-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1"},
				{"HEXPIRE", "h", "100000", "NX", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "200000", "NX", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "200000", "XX", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "100000", "GT", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "300000", "GT", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "100000", "LT", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "500000", "LT", "FIELDS", "1", "a"},
				{"HEXPIRE", "h", "100000", "FIELDS", "1", "missing"},
				{"HEXPIRE", "h", "100000", "FIELDS", "2", "a", "missing"},
			},
		},
		{
			Name: "hexpire-flags-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HEXPIRE", "h", "100000", "NX", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "200000", "NX", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "200000", "XX", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "100000", "GT", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "300000", "GT", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "100000", "LT", "FIELDS", "1", "f00100"},
					{"HEXPIRE", "h", "100000", "FIELDS", "1", "missing"},
					{"HEXPIRE", "h", "100000", "FIELDS", "2", "f00100", "missing"},
				},
			),
		},

		// HPERSIST returns 1 when it cleared a TTL, -1 when the field had none,
		// -2 when the field is gone. After it clears the TTL, HEXPIRETIME reads
		// -1. A missing key replies an all -2 array.
		{
			Name: "hpersist-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2"},
				{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "a"},
				{"HPERSIST", "h", "FIELDS", "1", "a"},
				{"HPERSIST", "h", "FIELDS", "1", "b"},
				{"HPERSIST", "h", "FIELDS", "1", "missing"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "a"},
				{"HPERSIST", "nokey", "FIELDS", "2", "a", "b"},
			},
		},
		{
			Name: "hpersist-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "f00100"},
					{"HPERSIST", "h", "FIELDS", "1", "f00100"},
					{"HPERSIST", "h", "FIELDS", "1", "f00101"},
					{"HPERSIST", "h", "FIELDS", "1", "missing"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "f00100"},
				},
			),
		},

		// A deadline in the past deletes the field and returns 2. Deleting the
		// last field deletes the key.
		{
			Name: "hexpireat-past-deletes-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2"},
				{"HEXPIREAT", "h", "1", "FIELDS", "1", "a"},
				{"HEXISTS", "h", "a"},
				{"HLEN", "h"},
				{"HEXPIREAT", "h", "1", "FIELDS", "2", "b", "missing"},
				{"EXISTS", "h"},
			},
		},
		{
			Name: "hexpireat-past-deletes-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HEXPIREAT", "h", "1", "FIELDS", "1", "f00100"},
					{"HEXISTS", "h", "f00100"},
					{"HLEN", "h"},
					{"HEXPIREAT", "h", "1", "FIELDS", "2", "f00101", "missing"},
					{"HLEN", "h"},
				},
			),
		},

		// HGETDEL reads the values then removes the fields. A missing field reads
		// a null in its slot. Removing every field deletes the key.
		{
			Name: "hgetdel-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2", "c", "3"},
				{"HGETDEL", "h", "FIELDS", "2", "a", "missing"},
				{"HEXISTS", "h", "a"},
				{"HLEN", "h"},
				{"HGETDEL", "h", "FIELDS", "2", "b", "c"},
				{"EXISTS", "h"},
				{"HGETDEL", "nokey", "FIELDS", "1", "a"},
			},
		},
		{
			Name: "hgetdel-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HGETDEL", "h", "FIELDS", "2", "f00100", "missing"},
					{"HEXISTS", "h", "f00100"},
					{"HLEN", "h"},
					{"HGETDEL", "h", "FIELDS", "2", "f00101", "f00102"},
					{"HLEN", "h"},
				},
			),
		},

		// HGETEX reads the values and changes the TTL. PERSIST clears it (HTTL
		// then reads -1), a PXAT in the past deletes the field, no option is a
		// plain read.
		{
			Name: "hgetex-inline",
			Steps: []Command{
				{"HSET", "h", "a", "1", "b", "2", "c", "3"},
				{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "a"},
				{"HGETEX", "h", "FIELDS", "1", "a"},
				{"HGETEX", "h", "PERSIST", "FIELDS", "1", "a"},
				{"HTTL", "h", "FIELDS", "1", "a"},
				{"HGETEX", "h", "EXAT", "9999999999", "FIELDS", "1", "b"},
				{"HEXPIRETIME", "h", "FIELDS", "1", "b"},
				{"HGETEX", "h", "PXAT", "1", "FIELDS", "1", "c"},
				{"HEXISTS", "h", "c"},
				{"HGETEX", "h", "FIELDS", "1", "missing"},
			},
		},
		{
			Name: "hgetex-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HEXPIREAT", "h", "9999999999", "FIELDS", "1", "f00100"},
					{"HGETEX", "h", "FIELDS", "1", "f00100"},
					{"HGETEX", "h", "PERSIST", "FIELDS", "1", "f00100"},
					{"HTTL", "h", "FIELDS", "1", "f00100"},
					{"HGETEX", "h", "EXAT", "9999999999", "FIELDS", "1", "f00101"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "f00101"},
					{"HGETEX", "h", "PXAT", "1", "FIELDS", "1", "f00102"},
					{"HEXISTS", "h", "f00102"},
					{"HGETEX", "h", "FIELDS", "1", "missing"},
				},
			),
		},

		// HPEXPIRE millisecond variants on a large hash, read back with HPTTL via
		// an absolute pin so the value is exact rather than a live countdown.
		{
			Name: "hpexpireat-absolute-large",
			Steps: join(
				[]Command{hsetBig("h", n)},
				[]Command{
					{"HPEXPIREAT", "h", "9999999999000", "FIELDS", "1", "f00100"},
					{"HPEXPIRETIME", "h", "FIELDS", "1", "f00100"},
					{"HEXPIRETIME", "h", "FIELDS", "1", "f00100"},
				},
			),
		},

		// Syntax and type errors, which both servers reject the same way.
		{
			Name: "hexpire-errors",
			Steps: []Command{
				{"HSET", "h", "a", "1"},
				{"HEXPIRE", "h", "1000", "a"},
				{"HEXPIRE", "h", "1000", "FIELDS", "2", "a"},
				{"SET", "s", "x"},
				{"HEXPIRE", "s", "1000", "FIELDS", "1", "a"},
				{"HTTL", "s", "FIELDS", "1", "a"},
			},
			Tolerate: map[int]Tolerance{
				1: ToleranceErrPrefix,
				2: ToleranceErrPrefix,
				4: ToleranceErrPrefix,
				5: ToleranceErrPrefix,
			},
		},
	}
}
