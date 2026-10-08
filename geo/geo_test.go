package geo

import "testing"

func TestBounds(t *testing.T) {
	if err := (Bounds{-123, 37, -121, 39}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Bounds{-121, 39, -123, 37}).Validate(); err == nil {
		t.Fatal("expected invalid bounds")
	}
}
