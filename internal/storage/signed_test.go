package storage

import (
	"regexp"
	"testing"
)

func TestSignedURLIsStableAndTiedToTheSecret(t *testing.T) {
	a := &Cloudinary{CloudName: "demo", APISecret: "secret-a"}
	b := &Cloudinary{CloudName: "demo", APISecret: "secret-b"}
	u := a.SignedURL("cribby/landlord-ids/abc")
	if !regexp.MustCompile(`^https://res\.cloudinary\.com/demo/image/authenticated/s--[A-Za-z0-9_-]{8}--/cribby/landlord-ids/abc$`).MatchString(u) {
		t.Fatalf("unexpected url %q", u)
	}
	if u != a.SignedURL("cribby/landlord-ids/abc") {
		t.Fatal("signing must be deterministic")
	}
	if u == b.SignedURL("cribby/landlord-ids/abc") {
		t.Fatal("a different secret must give a different signature")
	}
}
