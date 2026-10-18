package security

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// These fixtures were produced by the original Python stack (bcrypt.hashpw and
// python-jose jwt.encode, HS256 with secret "prod-secret-xyz"), so the tests
// prove existing password hashes and already-issued tokens keep working after
// the cutover to Go.
const (
	pyBcryptHash     = "$2b$12$zwi.AhmipUHQ2loKyNocfOrc39tMSybNITv21I8WyqzD8hg5OU.Fu" // bcrypt.hashpw("pässword-123")
	pyLongHash       = "$2b$12$VQHyZJrqJEC6AaCr6NAe0eITHEyNty.BPeq/.sQ128UL4qRz0Yr4O" // hash of the first 72 bytes of "x"*100
	pyToken          = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJtYXlhQHN0YXRldW5pdmVyc2l0eS5lZHUiLCJleHAiOjQxMDI0NDQ4MDB9.lrPzmVCWvesVTHe_CO8nWTFDy4C1YH5VL0Cr6mZVvR0"
	pyExpiredToken   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJtYXlhQHN0YXRldW5pdmVyc2l0eS5lZHUiLCJleHAiOjEwMDAwMDAwMDB9.QqVN7ifkbSLbTxzv2Tuz7LWYD-qyJ4q_pzxBXrsK0yk"
	pyWrongSecretTok = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJtYXlhQHN0YXRldW5pdmVyc2l0eS5lZHUiLCJleHAiOjQxMDI0NDQ4MDB9.7fDve_3hLlsCkTm6UEIPs3nzLZZWunU9ZKWCQAtBa1g"
	pySecret         = "prod-secret-xyz"
	pySubject        = "maya@stateuniversity.edu"
)

func TestVerifiesPythonBcryptHash(t *testing.T) {
	if !VerifyPassword("pässword-123", pyBcryptHash) {
		t.Fatal("Python-made bcrypt hash should verify")
	}
	if VerifyPassword("wrong", pyBcryptHash) {
		t.Fatal("wrong password verified")
	}
}

func TestPasswordsOver72BytesBehaveLikeBcrypt4(t *testing.T) {
	// Python bcrypt 4.x truncated to 72 bytes; hashes of long passwords exist in
	// the wild and must still verify (x/crypto would otherwise reject them).
	long := strings.Repeat("x", 100)
	if !VerifyPassword(long, pyLongHash) {
		t.Fatal("long-password hash from Python should verify")
	}
	BcryptCost = bcrypt.MinCost
	h, err := HashPassword(long)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(long, h) || !VerifyPassword(long[:72], h) {
		t.Fatal("round trip of a long password failed")
	}
}

func TestGoHashesAreReadableAsBcryptByPython(t *testing.T) {
	BcryptCost = bcrypt.MinCost
	h, _ := HashPassword("pw123456")
	if !strings.HasPrefix(h, "$2a$") && !strings.HasPrefix(h, "$2b$") {
		t.Fatalf("unexpected hash prefix: %s", h)
	}
	if len(h) != 60 {
		t.Fatalf("hash length %d", len(h))
	}
}

func TestDecodesPythonJoseToken(t *testing.T) {
	sub, ok := DecodeAccessToken(pySecret, pyToken)
	if !ok || sub != pySubject {
		t.Fatalf("sub=%q ok=%v", sub, ok)
	}
}

func TestRejectsExpiredWrongSecretAndGarbageTokens(t *testing.T) {
	for name, tok := range map[string]string{
		"expired": pyExpiredToken, "wrong secret": pyWrongSecretTok, "garbage": "not.a.jwt", "empty": "",
	} {
		if _, ok := DecodeAccessToken(pySecret, tok); ok {
			t.Errorf("%s token was accepted", name)
		}
	}
}

func TestRejectsNoneAlgorithm(t *testing.T) {
	// {"alg":"none"} with the right payload must not be accepted.
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJtYXlhQHN0YXRldW5pdmVyc2l0eS5lZHUiLCJleHAiOjQxMDI0NDQ4MDB9."
	if _, ok := DecodeAccessToken(pySecret, none); ok {
		t.Fatal("alg=none token was accepted")
	}
}

func TestTokenRoundTripAndExpiry(t *testing.T) {
	tok, err := CreateAccessToken("s3cret", "a@b.co", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if sub, ok := DecodeAccessToken("s3cret", tok); !ok || sub != "a@b.co" {
		t.Fatalf("sub=%q ok=%v", sub, ok)
	}
	if _, ok := DecodeAccessToken("different", tok); ok {
		t.Fatal("token verified with the wrong secret")
	}
	expired, _ := CreateAccessToken("s3cret", "a@b.co", -time.Minute)
	if _, ok := DecodeAccessToken("s3cret", expired); ok {
		t.Fatal("expired token was accepted")
	}
}

func TestCodes(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := GenerateCode()
		if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 150 {
		t.Fatalf("codes don't look random: only %d distinct of 200", len(seen))
	}
	// sha256 hex digest, the same format the Python app stored
	if got := HashCode("123456"); got != "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92" {
		t.Fatalf("hash %s", got)
	}
	if !CodesEqual("abc", "abc") || CodesEqual("abc", "abd") {
		t.Fatal("CodesEqual wrong")
	}
}

func TestNewUUIDLooksLikeV4(t *testing.T) {
	u := NewUUID()
	if len(u) != 36 || u[14] != '4' || !strings.ContainsRune("89ab", rune(u[19])) {
		t.Fatalf("bad uuid %s", u)
	}
	if NewUUID() == u {
		t.Fatal("uuids repeat")
	}
}
