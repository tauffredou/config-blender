package centralserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "cb_session"

// sessionTTL is how long a browser stays logged in after POST
// centralapi.LoginPath before it needs to log in again.
const sessionTTL = 7 * 24 * time.Hour

// sessionSigner issues and verifies session-cookie values without any
// server-side session store: a cookie is just an expiry timestamp plus an
// HMAC over it, keyed by a hash of the server's own write token. Anyone
// who can produce a valid cookie already had to know the write token (the
// same credential the Authorization header has always required) — a
// restart doesn't invalidate outstanding sessions, since the key is
// derived from the (stable, operator-configured) write token rather than
// generated fresh per process.
type sessionSigner struct {
	key []byte
}

func newSessionSigner(writeToken string) *sessionSigner {
	key := sha256.Sum256([]byte("configblender-session-v1:" + writeToken))
	return &sessionSigner{key: key[:]}
}

// issue returns a new, currently-valid signed cookie value.
func (s *sessionSigner) issue() string {
	return s.sign(time.Now().Add(sessionTTL).Unix())
}

func (s *sessionSigner) sign(expiresAt int64) string {
	payload := strconv.FormatInt(expiresAt, 10)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// valid reports whether v is a cookie this signer issued, and not expired.
func (s *sessionSigner) valid(v string) bool {
	payload, _, ok := strings.Cut(v, ".")
	if !ok {
		return false
	}
	expiresAt, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() >= expiresAt {
		return false
	}
	want := s.sign(expiresAt)
	return subtle.ConstantTimeCompare([]byte(v), []byte(want)) == 1
}
