// Package geo holds the distance helpers shared by proximity search across
// resources (listings, places).
package geo

import (
	"math"
	"sort"
)

// EarthRadiusKM is the mean Earth radius (IUGG value) — good enough for
// "how close is this".
const EarthRadiusKM = 6371.0088

// HaversineKM is the great-circle ("as the crow flies") distance between two
// lat/lng points, in kilometres.
func HaversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	phi1, phi2 := lat1*rad, lat2*rad
	dPhi := (lat2 - lat1) * rad
	dLambda := (lon2 - lon1) * rad
	a := math.Pow(math.Sin(dPhi/2), 2) + math.Cos(phi1)*math.Cos(phi2)*math.Pow(math.Sin(dLambda/2), 2)
	return 2 * EarthRadiusKM * math.Asin(math.Sqrt(a))
}

// Round2 rounds to two decimals, like Python's round(x, 2).
func Round2(x float64) float64 { return math.Round(x*100) / 100 }

// Point is anything with optional coordinates and a settable distance.
type Point interface {
	Coords() (lat, lng *float64)
	SetDistance(km float64)
	Distance() float64
}

// RankByDistance stamps each item with its distance from the origin, drops
// items with no coordinates (or beyond radiusKM, if non-nil), and returns the
// rest nearest-first. Done in Go rather than SQL because SQLite has no trig
// functions and these tables are small.
func RankByDistance[T Point](rows []T, originLat, originLng float64, radiusKM *float64) []T {
	ranked := make([]T, 0, len(rows))
	for _, row := range rows {
		lat, lng := row.Coords()
		if lat == nil || lng == nil {
			continue
		}
		row.SetDistance(Round2(HaversineKM(originLat, originLng, *lat, *lng)))
		if radiusKM != nil && row.Distance() > *radiusKM {
			continue
		}
		ranked = append(ranked, row)
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Distance() < ranked[j].Distance() })
	return ranked
}
