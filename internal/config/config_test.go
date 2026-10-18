package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultsMatchThePythonSettings(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "SECRET_KEY", "BACKEND_CORS_ORIGINS", "PORT", "EMAIL_FROM"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Chdir(t.TempDir())
	c := Load()
	if c.DatabaseURL != "sqlite+aiosqlite:///./campus.db" || c.APIV1Str != "/api/v1" || c.AccessTokenExpireMinutes != 43200 ||
		c.EmailVerifyExpireMinutes != 30 || c.EmailVerifyCooldownSeconds != 60 || c.Port != "8000" {
		t.Fatalf("got %+v", c)
	}
	if len(c.BackendCORSOrigins) != 5 || c.BackendCORSOrigins[4] != "https://admin.cribby.space" {
		t.Fatalf("cors %v", c.BackendCORSOrigins)
	}
}

func TestDotEnvIsLoadedButRealEnvWins(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	os.Unsetenv("RESEND_API_KEY")
	os.Unsetenv("EMAIL_VERIFICATION_TOKEN_EXPIRE_MINUTES")
	os.WriteFile(filepath.Join(dir, ".env"), []byte("# comment\nRESEND_API_KEY=\"from-file\"\nEMAIL_VERIFICATION_TOKEN_EXPIRE_MINUTES=10\nSECRET_KEY=file-secret\n"), 0o600)
	t.Setenv("SECRET_KEY", "env-secret")
	c := Load()
	if c.ResendAPIKey != "from-file" || c.EmailVerifyExpireMinutes != 10 || c.SecretKey != "env-secret" {
		t.Fatalf("got key=%q mins=%d secret=%q", c.ResendAPIKey, c.EmailVerifyExpireMinutes, c.SecretKey)
	}
}

func TestCORSOriginsAcceptJSONOrCSV(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("BACKEND_CORS_ORIGINS", `["https://a.com","https://b.com"]`)
	if got := Load().BackendCORSOrigins; !reflect.DeepEqual(got, []string{"https://a.com", "https://b.com"}) {
		t.Fatalf("json: %v", got)
	}
	t.Setenv("BACKEND_CORS_ORIGINS", "https://c.com, https://d.com")
	if got := Load().BackendCORSOrigins; !reflect.DeepEqual(got, []string{"https://c.com", "https://d.com"}) {
		t.Fatalf("csv: %v", got)
	}
}
