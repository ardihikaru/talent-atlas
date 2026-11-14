package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	passwordIterations = 600_000
	sessionCookie      = "education_session"
	sessionTTL         = 30 * 24 * time.Hour
)

// NewID returns a random 128-bit id with a readable prefix.
func NewID(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

// HashPassword derives a PBKDF2-SHA256 hash: iterations$salt$hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword compares a password with a stored hash in constant time.
func CheckPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 {
		return false
	}
	iterations, err := strconv.Atoi(parts[0])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// Sessions signs and verifies the session cookie.
type Sessions struct {
	Secret []byte
	Secure bool
}

func (s Sessions) sign(payload string) string {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Issue sets a signed cookie for the user.
func (s Sessions) Issue(w http.ResponseWriter, userID string) {
	payload := userID + "." + strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: payload + "." + s.sign(payload), Path: "/",
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

// Clear removes the cookie.
func (s Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.Secure, MaxAge: -1})
}

// UserID returns the signed-in user or an error.
func (s Sessions) UserID(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(c.Value, ".")
	if i < 0 {
		return "", errors.New("malformed session")
	}
	payload, sig := c.Value[:i], c.Value[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return "", errors.New("bad session signature")
	}
	j := strings.LastIndex(payload, ".")
	if j < 0 {
		return "", errors.New("malformed session")
	}
	expires, err := strconv.ParseInt(payload[j+1:], 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return "", errors.New("session expired")
	}
	return payload[:j], nil
}
