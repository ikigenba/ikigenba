package wiki

import "testing"

func TestSpecDeclaresServedMCPService(t *testing.T) {
	// R-MWBI-4JY7
	spec := Spec()
	if spec.App != "wiki" {
		t.Fatalf("App = %q, want wiki", spec.App)
	}
	if spec.Mount != "/srv/wiki/" {
		t.Fatalf("Mount = %q, want /srv/wiki/", spec.Mount)
	}
	if spec.Port != 3006 {
		t.Fatalf("Port = %d, want 3006", spec.Port)
	}
	if !spec.MCP {
		t.Fatal("MCP = false, want true")
	}
	if spec.Handlers == nil {
		t.Fatal("Handlers is nil; service would not mount /mcp")
	}
}
