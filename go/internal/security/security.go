// Package security covers password hashing, JWT access tokens and the
// 6-digit one-time codes used for email verification and password reset.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// BcryptCost matches Python's bcrypt.gensalt() default. Tests lower it.
var BcryptCost = 12

// bcrypt only uses the first 72 bytes. The Python bcrypt 4.x silently
// truncated longer passwords, so existing hashes were made that way — keep
// doing the same so those users can still log in.
func prep(p string) []byte {
	b := []byte(p)
	if len(b) > 72 {
		b = b[:72]
	}
	return b
}

func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword(prep(password), BcryptCost)
	return string(h), err
}

func VerifyPassword(plain, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), prep(plain)) == nil
}

const algorithm = "HS256"

func CreateAccessToken(secret, subject string, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{"sub": subject, "exp": time.Now().UTC().Add(ttl).Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// DecodeAccessToken returns the token's subject, or ok=false if the token is
// invalid, expired or signed with a different algorithm/secret.
func DecodeAccessToken(secret, token string) (subject string, ok bool) {
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{algorithm}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return "", false
	}
	claims, _ := parsed.Claims.(jwt.MapClaims)
	sub, _ := claims["sub"].(string)
	return sub, sub != ""
}

func GenerateCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func HashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func CodesEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// NewUUID returns a random (version 4) UUID string.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
