package places

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

func object(data []byte) map[string]any {
	var obj map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	if d.Decode(&obj) != nil || d.Decode(new(any)) != io.EOF {
		return nil
	}
	return obj
}

func decodePlaces(data []byte) ([]Place, bool) {
	obj := object(data)
	if obj == nil {
		return nil, false
	}
	out := []Place{}
	raw, exists := obj["places"]
	if !exists {
		return out, true
	}
	list, ok := raw.([]any)
	if !ok || len(list) > MaxResults {
		return nil, false
	}
	for _, item := range list {
		p, ok := decodePlace(item)
		if !ok {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

func decodePlace(item any) (Place, bool) {
	p := Place{}
	obj, ok := item.(map[string]any)
	if !ok {
		return p, false
	}
	p.ID, ok = obj["id"].(string)
	if !ok || p.ID == "" {
		return Place{}, false
	}
	if raw, exists := obj["displayName"]; exists {
		display, ok := raw.(map[string]any)
		if !ok {
			return Place{}, false
		}
		if raw, exists := display["text"]; exists {
			p.Name, ok = raw.(string)
			if !ok {
				return Place{}, false
			}
		}
	}
	for key, dst := range map[string]*string{"formattedAddress": &p.Address, "primaryType": &p.PrimaryType, "websiteUri": &p.Website, "nationalPhoneNumber": &p.Phone} {
		if raw, exists := obj[key]; exists {
			*dst, ok = raw.(string)
			if !ok {
				return Place{}, false
			}
		}
	}
	return p, true
}

func refusal(data []byte, key string) string {
	err, _ := object(data)["error"].(map[string]any)
	s, _ := err["status"].(string)
	if len(s) < 1 || len(s) > 64 || (key != "" && strings.Contains(s, key)) {
		return ""
	}
	for i := range len(s) {
		if (s[i] < 'A' || s[i] > 'Z') && s[i] != '_' {
			return ""
		}
	}
	return s
}
