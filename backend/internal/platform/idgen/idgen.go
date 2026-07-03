// Package idgen generates client-side UUIDv4 identifiers, needed anywhere
// an ID must exist before an INSERT (e.g. a refresh-token family_id shared
// across rotations) rather than being assigned by Postgres's
// gen_random_uuid() default.
package idgen

import (
	"crypto/rand"
	"fmt"
)

func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
