package api_test

import (
	"context"
	"testing"
	"time"
)

func TestSavedSearchAlerts(t *testing.T) {
	e := newEnv(t)
	tok, u := e.signup("kemi@gmail.com")
	other, _ := e.signup("ola@gmail.com")
	e.createListingAt("Old flat", nil, nil) // ₦800, already live
	ctx := context.Background()

	// Only people with a confirmed email can get alerts.
	e.expect(403, "POST", "/saved-searches", tok, obj{"name": "Cheap", "max_price": 900})
	e.verifyUser(id(u))

	e.expect(422, "POST", "/saved-searches", tok, obj{"name": "x", "water": "mud"})
	cheap := id(e.expect(201, "POST", "/saved-searches", tok, obj{"name": "Cheap", "max_price": 900}).(obj))
	dear := id(e.expect(201, "POST", "/saved-searches", tok, obj{"name": "Posh", "max_price": 500}).(obj))
	_ = dear

	// What already matched is old news.
	e.api.SweepSearchAlerts(ctx, time.Now().UTC())
	if len(e.mail.searchAlerts) != 0 {
		t.Fatalf("nothing is new yet: %v", e.mail.searchAlerts)
	}

	// A new home that fits only the first search.
	e.createListingAt("Brand new studio", nil, nil)
	e.api.SweepSearchAlerts(ctx, time.Now().UTC())
	if len(e.mail.searchAlerts) != 1 {
		t.Fatalf("one alert expected: %v", e.mail.searchAlerts)
	}
	a := e.mail.searchAlerts[0]
	if a.To != "kemi@gmail.com" || a.Name != "Cheap" || a.Total != 1 || len(a.Titles) != 1 || a.Titles[0] != "Brand new studio" {
		t.Fatalf("alert: %+v", a)
	}
	e.api.SweepSearchAlerts(ctx, time.Now().UTC().Add(time.Hour))
	if len(e.mail.searchAlerts) != 1 {
		t.Fatal("the same home is never announced twice")
	}

	// Two more homes: nothing for a day after the last email, then one email about both.
	e.createListingAt("Third", nil, nil)
	e.createListingAt("Fourth", nil, nil)
	e.api.SweepSearchAlerts(ctx, time.Now().UTC().Add(2*time.Hour))
	if len(e.mail.searchAlerts) != 1 {
		t.Fatal("at most one email a day")
	}
	e.api.SweepSearchAlerts(ctx, time.Now().UTC().Add(25*time.Hour))
	if len(e.mail.searchAlerts) != 2 || e.mail.searchAlerts[1].Total != 2 {
		t.Fatalf("a day later both arrive in one email: %v", e.mail.searchAlerts)
	}

	// Hidden or unapproved homes never trigger an alert.
	e.createListingAt("Secret", nil, nil)
	e.exec(`UPDATE listings SET status = 'pending' WHERE title = 'Secret'`)
	e.api.SweepSearchAlerts(ctx, time.Now().UTC().Add(60*time.Hour))
	if len(e.mail.searchAlerts) != 2 {
		t.Fatal("a pending home must not be announced")
	}

	// A home approved later is announced then.
	e.exec(`UPDATE listings SET status = 'approved' WHERE title = 'Secret'`)
	e.api.SweepSearchAlerts(ctx, time.Now().UTC().Add(61*time.Hour))
	if len(e.mail.searchAlerts) != 3 || e.mail.searchAlerts[2].Titles[0] != "Secret" {
		t.Fatalf("approval later: %v", e.mail.searchAlerts)
	}

	// Own searches only; the limit is five.
	if got := list(e.expect(200, "GET", "/saved-searches", tok, nil)); len(got) != 2 {
		t.Fatalf("two saved searches: %v", got)
	}
	e.expect(404, "DELETE", path("/saved-searches/%d", cheap), other, nil)
	e.expect(204, "DELETE", path("/saved-searches/%d", cheap), tok, nil)
	e.expect(404, "DELETE", path("/saved-searches/%d", cheap), tok, nil)
	for i := 0; i < 4; i++ {
		e.expect(201, "POST", "/saved-searches", tok, obj{"name": "More"})
	}
	e.expect(409, "POST", "/saved-searches", tok, obj{"name": "One too many"})

	// Deleting the account takes the searches with it.
	e.expect(204, "DELETE", "/users/me", tok, obj{"password": "pw123456"})
	if e.count(`SELECT COUNT(*) FROM saved_searches`) != 0 || e.count(`SELECT COUNT(*) FROM saved_search_seen`) != 0 {
		t.Fatal("saved searches should go with the account")
	}
}
