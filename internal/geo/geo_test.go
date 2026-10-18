package geo

import "testing"

func TestHaversineKnownDistance(t *testing.T) {
	// Lagos (Marina) -> Abuja (city centre) is ~525 km as the crow flies.
	km := HaversineKM(6.4550, 3.3841, 9.0765, 7.3986)
	if km < 500 || km > 545 {
		t.Fatalf("got %v km", km)
	}
}

func TestHaversineZeroDistance(t *testing.T) {
	if d := HaversineKM(6.5158, 3.3898, 6.5158, 3.3898); d != 0 {
		t.Fatalf("got %v", d)
	}
}
