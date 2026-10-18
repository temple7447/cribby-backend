// Package schools embeds the curated list of Nigerian universities,
// polytechnics and colleges of education (built by schools-data/build_schools.py)
// so every deployment gets the same school directory with no manual import step.
package schools

import (
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"strconv"
	"strings"
)

//go:embed schools.csv
var raw []byte

// Row is one institution as shipped in the CSV. Latitude/Longitude are nil for
// the few institutions no source could place on a map.
type Row struct {
	Name, Type, State, Town, Funding string
	Latitude, Longitude              *float64
	// Confidence is how trustworthy the coordinates are: high/medium come from
	// Wikidata or a precise search hit; low is the centre of the school's town and
	// very_low the centre of its state — so distances are approximate.
	Confidence, Source string
}

// Version identifies this dataset; it changes whenever the CSV does.
func Version() string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func Rows() ([]Row, error) {
	recs, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		return nil, err
	}
	var out []Row
	for _, r := range recs[1:] { // name,type,state,town,funding,latitude,longitude,confidence,source
		row := Row{Name: r[0], Type: r[1], State: r[2], Town: r[3], Funding: r[4], Confidence: r[7], Source: r[8]}
		if lat, err := strconv.ParseFloat(r[5], 64); err == nil {
			row.Latitude = &lat
		}
		if lng, err := strconv.ParseFloat(r[6], 64); err == nil {
			row.Longitude = &lng
		}
		out = append(out, row)
	}
	return out, nil
}
