package auth

import (
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA-1 rows), truncated from the RFC's
// 8 digits to our 6 — the standard's own truncation just takes the value
// mod 10^digits, so the last 6 digits of each vector are the expected code.
func TestTOTPCode_RFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},          // RFC: 94287082
		{1111111109, "081804"},  // RFC: 07081804
		{1111111111, "050471"},  // RFC: 14050471
		{1234567890, "005924"},  // RFC: 89005924
		{2000000000, "279037"},  // RFC: 69279037
		{20000000000, "353130"}, // RFC: 65353130
	}
	for _, v := range vectors {
		step := v.unix / int64(totpPeriod.Seconds())
		if got := totpCode(secret, step); got != v.want {
			t.Errorf("totpCode at t=%d: got %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestVerifyTOTP_AcceptsSkewRejectsBeyond(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(1111111111, 0)
	step := now.Unix() / int64(totpPeriod.Seconds())
	key := []byte("12345678901234567890")

	for delta := int64(-1); delta <= 1; delta++ {
		if _, ok := verifyTOTP(secret, totpCode(key, step+delta), now); !ok {
			t.Errorf("code for step offset %d should verify within skew", delta)
		}
	}
	for _, delta := range []int64{-2, 2} {
		if _, ok := verifyTOTP(secret, totpCode(key, step+delta), now); ok {
			t.Errorf("code for step offset %d should NOT verify", delta)
		}
	}
	if _, ok := verifyTOTP(secret, "000000", now); ok {
		t.Error("all-zero code should not verify")
	}
}
