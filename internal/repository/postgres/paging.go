package postgres

import "math"

// int32Bound narrows a limit or offset onto the int4 that SQL's LIMIT and
// OFFSET take.
//
// Go's int is 64 bits here, so a plain int32(n) wraps anything above
// math.MaxInt32 to a negative number, and Postgres rejects a negative LIMIT
// (SQLSTATE 2201W) and a negative OFFSET (2201X) outright — the page came back
// as a database error instead of as rows. That is reachable from the API:
// handler.parseOffset puts no upper bound on ?offset=, so ?offset=2147483648
// was a 500.
//
// Clamping rather than erroring gives the caller what it asked for: a limit
// past MaxInt32 means "no ceiling", and an offset past MaxInt32 is a page
// beyond the end of anything these tables hold, so it yields none. Negative
// values are nonsense no caller produces, and clamp to zero.
//
// The bound lives here rather than in the handler because this is where the
// narrowing happens. The handler's cap of 100 is one caller's policy, and not
// every caller goes through it — cache.FeedCache calls ListPosts directly.
func int32Bound(n int) int32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(n)
	}
}
