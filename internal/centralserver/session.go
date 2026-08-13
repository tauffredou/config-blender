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
// server-side session store: a cookie is a subject (a username, or ""
// for the break-glass write-token identity — server.go's authenticate)
// plus an expiry timestamp, HMACed with a random secret persisted in the
// Recipe database (recipesource.Store.SessionSecret). Persisted rather
// than derived from the write token (as an earlier version of this did):
// with real per-user accounts, sessions must stay unforgeable even when
// no write token is configured at all.
type sessionSigner struct {
	key []byte
}

func newSessionSigner(secret []byte) *sessionSigner {
	key := sha256.Sum256(append([]byte("configblender-session-v1:"), secret...))
	return &sessionSigner{key: key[:]}
}

// issueFor returns a new, currently-valid signed cookie value naming
// subject as the session's identity.
func (s *sessionSigner) issueFor(subject string) string {
	return s.sign(subject, time.Now().Add(sessionTTL).Unix())
}

func (s *sessionSigner) sign(subject string, expiresAt int64) string {
	payload := subject + "|" + strconv.FormatInt(expiresAt, 10)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// valid reports whether v is a cookie this signer issued, and not expired,
// returning the subject it was issued for (userdb usernames can't contain
// "|" — enforced by userdb.Store.Create — so splitting on the last "|"
// before the signature separator is unambiguous).
func (s *sessionSigner) valid(v string) (subject string, ok bool) {
	payload, _, ok := strings.Cut(v, ".")
	if !ok {
		return "", false
	}
	idx := strings.LastIndex(payload, "|")
	if idx < 0 {
		return "", false
	}
	subject, expiresRaw := payload[:idx], payload[idx+1:]
	expiresAt, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().Unix() >= expiresAt {
		return "", false
	}
	want := s.sign(subject, expiresAt)
	if subtle.ConstantTimeCompare([]byte(v), []byte(want)) != 1 {
		return "", false
	}
	return subject, true
}
