// Package geo provides geographical bounds validation and longitude normalization.
package geo

import (
	"errors"
	"math"
)

type Bounds struct{ West, South, East, North float64 }

func (b Bounds) Validate() error {
	for _, v := range []float64{b.West, b.South, b.East, b.North} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("non-finite coordinate")
		}
	}
	if b.West < -180 || b.East > 180 || b.South < -90 || b.North > 90 || b.West >= b.East || b.South >= b.North {
		return errors.New("invalid geographic bounds (antimeridian crossings not supported)")
	}
	return nil
}
