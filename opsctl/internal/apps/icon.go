package apps

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	// IconPath is the launcher icon's path beneath an app root.
	IconPath = "share/icon.svg"
	// ServicesPath is the published launcher document's host path.
	ServicesPath = "/var/lib/ikigenba/services.json"
	// ServicesEnv names the environment value containing the launcher document.
	ServicesEnv = "IKIGENBA_SERVICES"
)

var (
	// ErrIconNotSVG reports an invalid launcher icon.
	ErrIconNotSVG = errors.New("share/icon.svg is not an SVG image")
	// ErrIconTooLarge reports an icon exceeding 64 KiB.
	ErrIconTooLarge = errors.New("share/icon.svg is larger than 64 KiB")
)

// CheckIcon validates the complete contents of a launcher icon.
func CheckIcon(data []byte) error {
	if len(data) > 65536 {
		return ErrIconTooLarge
	}
	if !utf8.Valid(data) {
		return ErrIconNotSVG
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) && rootSeen && depth == 0 {
				return nil
			}
			return ErrIconNotSVG
		}
		switch item := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootSeen || item.Name.Local != "svg" {
					return ErrIconNotSVG
				}
				rootSeen = true
			}
			seen := make(map[xml.Name]bool, len(item.Attr))
			for _, attr := range item.Attr {
				if seen[attr.Name] {
					return ErrIconNotSVG
				}
				seen[attr.Name] = true
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 {
				for _, character := range item {
					if character != ' ' && character != '\t' && character != '\r' && character != '\n' {
						return ErrIconNotSVG
					}
				}
			}
		}
	}
}
