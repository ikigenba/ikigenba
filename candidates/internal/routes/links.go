package routes

import "net/url"

// PlaceURL opens Google Maps on the resolved address.
func PlaceURL(address string) string {
	if address == "" {
		return ""
	}
	return "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(address)
}

// DirectionsURL opens driving directions between the supplied addresses.
func DirectionsURL(origin, destination string) string {
	if origin == "" || destination == "" {
		return ""
	}
	return "https://www.google.com/maps/dir/?api=1&origin=" + url.QueryEscape(origin) +
		"&destination=" + url.QueryEscape(destination) + "&travelmode=driving"
}
