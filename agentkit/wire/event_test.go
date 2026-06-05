package wire_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-TSL0-SJTK: every emitted event has a top-level `type` string
// discriminator; the v1 emitted set is exactly {assistant, user,
// result}. Constructors must fix the `type` field so callers cannot
// produce a malformed event by forgetting the discriminator.
func TestR_TSL0_SJTK_EventTypeDiscriminator(t *testing.T) {
	// Each constructor pins its own type. Round-trip through Encode
	// and assert the top-level "type" matches.
	cases := []struct {
		name string
		want string
		make func() (any, error)
	}{
		{
			name: "assistant",
			want: "assistant",
			make: func() (any, error) {
				return wire.NewAssistantEvent(), nil
			},
		},
		{
			name: "user",
			want: "user",
			make: func() (any, error) {
				return wire.NewUserEvent(), nil
			},
		},
		{
			name: "result",
			want: "result",
			make: func() (any, error) {
				return wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
			},
		},
	}

	emitted := map[string]bool{}
	for _, tc := range cases {
		ev, err := tc.make()
		if err != nil {
			t.Fatalf("%s: construct: %v", tc.name, err)
		}
		var buf bytes.Buffer
		if err := wire.Encode(&buf, ev); err != nil {
			t.Fatalf("%s: encode: %v", tc.name, err)
		}
		line := strings.TrimSuffix(buf.String(), "\n")
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		gotType, ok := got["type"].(string)
		if !ok {
			t.Fatalf("%s: top-level type missing or non-string: %v", tc.name, got["type"])
		}
		if gotType != tc.want {
			t.Errorf("%s: type = %q, want %q", tc.name, gotType, tc.want)
		}
		emitted[gotType] = true
	}

	// The v1 emitted set must be exactly {assistant, user, result}.
	got := make([]string, 0, len(emitted))
	for k := range emitted {
		got = append(got, k)
	}
	sort.Strings(got)

	want := append([]string(nil), wire.EmittedTypes...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("emitted set = %v, want %v", got, want)
	}
	if strings.Join(want, ",") != "assistant,result,user" {
		t.Errorf("EmittedTypes = %v, want exactly {assistant, user, result}", want)
	}
}
