package main

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-S2GL-3PZS
func TestLauncherIconIsSVGDocument(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(mainProjectRoot(t), "share", "icon.svg"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(contents)))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse SVG: %v", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if value.Name != (xml.Name{Space: "http://www.w3.org/2000/svg", Local: "svg"}) {
					t.Errorf("root = %v", value.Name)
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				t.Error("text outside SVG root")
			}
		}
	}
	if roots != 1 || depth != 0 {
		t.Errorf("roots=%d depth=%d", roots, depth)
	}
}
