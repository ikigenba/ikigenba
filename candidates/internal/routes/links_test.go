package routes_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/routes"
)

// R-YB6H-NAIX R-YDMA-EU0B
func TestPlaceURL(t *testing.T) {
	cases := []struct{ address, want string }{
		{"", ""},
		{"12 Main St, Sandstone, MN 55072, USA", "https://www.google.com/maps/search/?api=1&query=12+Main+St%2C+Sandstone%2C+MN+55072%2C+USA"},
		{"A&B #1", "https://www.google.com/maps/search/?api=1&query=A%26B+%231"},
		{" +%/?=", "https://www.google.com/maps/search/?api=1&query=+%2B%25%2F%3F%3D"},
		{"Montréal\n\x00", "https://www.google.com/maps/search/?api=1&query=Montr%C3%A9al%0A%00"},
	}
	for _, tc := range cases {
		if got := routes.PlaceURL(tc.address); got != tc.want {
			t.Errorf("PlaceURL(%q) = %q, want %q", tc.address, got, tc.want)
		}
	}
}

// R-YCEE-129M R-YEU6-SLR0
func TestDirectionsURL(t *testing.T) {
	cases := []struct{ origin, destination, want string }{
		{"", "", ""},
		{"", "A&B #1", ""},
		{"Sandstone, MN", "", ""},
		{"Sandstone, MN", "12 Main St, Hinckley, MN 55037, USA", "https://www.google.com/maps/dir/?api=1&origin=Sandstone%2C+MN&destination=12+Main+St%2C+Hinckley%2C+MN+55037%2C+USA&travelmode=driving"},
		{"Sandstone, MN", "A&B #1", "https://www.google.com/maps/dir/?api=1&origin=Sandstone%2C+MN&destination=A%26B+%231&travelmode=driving"},
		{"Montréal\n\x00", " +%/?=", "https://www.google.com/maps/dir/?api=1&origin=Montr%C3%A9al%0A%00&destination=+%2B%25%2F%3F%3D&travelmode=driving"},
		{"A&B #1", "Montréal", "https://www.google.com/maps/dir/?api=1&origin=A%26B+%231&destination=Montr%C3%A9al&travelmode=driving"},
	}
	for _, tc := range cases {
		if got := routes.DirectionsURL(tc.origin, tc.destination); got != tc.want {
			t.Errorf("DirectionsURL(%q, %q) = %q, want %q", tc.origin, tc.destination, got, tc.want)
		}
	}
}
