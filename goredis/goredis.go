// Package goredis is a placeholder for library-level compatibility checks run
// through the go-redis client (github.com/redis/go-redis). The native respwire
// path covers exact wire framing; this package is where we will later assert
// that a popular real-world client driver round-trips against aki the same way
// it does against Redis and Valkey.
//
// It is a stub on purpose. Pulling in go-redis is a real dependency decision and
// the wire-level differential path does not need it, so we add it only when the
// library-level checks land. Until then this package documents the intent and
// keeps the import path reserved.
package goredis

// Planned is a marker that records this package is reserved for the go-redis
// based library compatibility suite. It carries no behavior yet.
const Planned = true
