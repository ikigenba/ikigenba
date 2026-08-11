package artifacts

import "testing"

// R-4SFL-3XV0
func TestEventReflectionDeclaresExactFamiliesSubjectsSamplesAndFilter(t *testing.T) {
	want := []string{"created", "updated", "deleted"}
	index := Events.Index()
	if len(index) != len(want) {
		t.Fatalf("event families = %#v", index)
	}
	for i, kind := range want {
		if index[i]["kind"] != kind || index[i]["subject"] != "/<artifact id>" {
			t.Errorf("family %d = %#v", i, index[i])
		}
		detail, err := Events.Detail(kind)
		if err != nil {
			t.Fatal(err)
		}
		example, ok := detail["example"].(map[string]any)
		if !ok || example["id"] == "" || example["owner_id"] == "" || example["owner_email"] == "" {
			t.Errorf("%s payload sample = %#v", kind, detail["example"])
		}
	}
	accepted, err := Events.CouldMatch("artifacts", "artifacts:created/**")
	if err != nil || !accepted {
		t.Fatalf("created filter accepted = %v, %v", accepted, err)
	}
}
