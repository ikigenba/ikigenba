package cli

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLauncherIconXML(t *testing.T) {
	// R-SW8J-X7VH
	file, err := os.Open(filepath.Join("..", "..", "share", "icon.svg"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	decoder := xml.NewDecoder(file)
	roots, depth := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch v := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if v.Name.Local != "svg" || v.Name.Space != "http://www.w3.org/2000/svg" {
					t.Fatalf("root = %v", v.Name)
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 {
				for _, b := range v {
					if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
						t.Fatal("text outside root")
					}
				}
			}
		}
	}
	if roots != 1 || depth != 0 {
		t.Fatalf("roots=%d depth=%d", roots, depth)
	}
}
