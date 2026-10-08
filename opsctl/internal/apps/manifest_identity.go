package apps

import "unicode/utf8"

// ManifestDisowns reports whether valid TOML names an app other than service.
// Malformed TOML is an installed manifest fault, rather than absent identity.
func ManifestDisowns(data []byte, service string) bool {
	if !utf8.Valid(data) || validateLineEndings(data) != nil {
		return false
	}
	decoder := newManifestDecoder(data)
	decoder.identityOnly = true
	if decoder.decode() != nil || decoder.syntaxFault {
		return false
	}
	return decoder.result.App != service
}
