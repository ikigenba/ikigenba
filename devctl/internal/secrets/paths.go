package secrets

// Parameter returns the Parameter Store name for an app's secrets object.
func Parameter(domain, app string) string {
	return Prefix(domain) + "/" + app
}

// Prefix returns the Parameter Store prefix for a space's secrets objects.
func Prefix(domain string) string {
	return "/" + domain
}
