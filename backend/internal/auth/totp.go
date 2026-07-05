package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

// TOTP per RFC 6238 (HMAC-SHA1, 6 digits, 30s steps — the parameters every
// authenticator app defaults to), implemented on the stdlib rather than
// pulling in an OTP dependency; the whole algorithm is HOTP (RFC 4226) over
// a time counter.
const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSkew accepts the previous/next time step too, absorbing clock
	// drift between the server and the user's phone.
	totpSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() string {
	b := make([]byte, 20) // 160-bit, RFC 4226 §4 recommended
	_, _ = rand.Read(b)
	return b32.EncodeToString(b)
}

func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000)
}

// verifyTOTP checks code against the ±totpSkew window around now and
// returns the matched time step, so callers can reject replays of the same
// step (see Service.verifyTOTPOnce).
func verifyTOTP(secret string, code string, now time.Time) (step int64, ok bool) {
	key, err := b32.DecodeString(secret)
	if err != nil {
		return 0, false
	}
	current := now.Unix() / int64(totpPeriod.Seconds())
	for delta := int64(-totpSkew); delta <= totpSkew; delta++ {
		s := current + delta
		if subtle.ConstantTimeCompare([]byte(totpCode(key, s)), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// otpauthURI is what enrollment QR codes encode — the de-facto standard
// authenticator-app provisioning format.
func otpauthURI(email, secret string) string {
	return fmt.Sprintf("otpauth://totp/Nimbus:%s?secret=%s&issuer=Nimbus&algorithm=SHA1&digits=%d&period=%d",
		url.PathEscape(email), secret, totpDigits, int(totpPeriod.Seconds()))
}
