package places

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

// Fake answers Nearby requests entirely in process.
type Fake struct{}

// RoundTrip answers valid circle requests with deterministic synthetic places.
func (Fake) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.Method != http.MethodPost || r.URL == nil || r.URL.String() != NearbyURL || r.Body == nil {
		return fakeResponse(http.StatusNotFound, ""), nil
	}
	defer func() { _ = r.Body.Close() }()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return fakeResponse(http.StatusNotFound, ""), nil
	}
	var obj map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&obj) != nil || decoder.Decode(new(any)) != io.EOF {
		return fakeResponse(http.StatusNotFound, ""), nil
	}
	lr, _ := obj["locationRestriction"].(map[string]any)
	circle, _ := lr["circle"].(map[string]any)
	radius, ok := circle["radius"].(json.Number)
	if !ok {
		return fakeResponse(http.StatusNotFound, ""), nil
	}
	center, _ := circle["center"].(map[string]any)
	lat, _ := center["latitude"].(json.Number)
	lon, _ := center["longitude"].(json.Number)
	count := FakeFew
	if decimalAtLeast(radius, json.Number(strconv.Itoa(FakeFullRadius))) {
		count = MaxResults
	}
	// Decimal representations keep distinct circles distinct without a hash collision.
	prefix := "fake:" + decimalWord(lat) + ":" + decimalWord(lon) + ":" + decimalWord(radius) + ":"
	list := make([]map[string]any, count)
	for i := range list {
		list[i] = map[string]any{"id": prefix + strconv.Itoa(i), "displayName": map[string]string{"text": "Sandbox business " + strconv.Itoa(i+1)}, "formattedAddress": "Sandbox street"}
	}
	body, _ := json.Marshal(map[string]any{"places": list})
	return fakeResponse(http.StatusOK, string(body)), nil
}

// decimalParts normalizes JSON decimals without expanding their exponent.
// This keeps arbitrarily large exponents cheap and preserves exact identity.
func decimalParts(n json.Number) (digits string, exponent *big.Int) {
	text := string(n)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	exponent = new(big.Int)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent.SetString(text[i+1:], 10)
		text = text[:i]
	}
	if i := strings.IndexByte(text, '.'); i >= 0 {
		exponent.Sub(exponent, big.NewInt(int64(len(text)-i-1)))
		text = text[:i] + text[i+1:]
	}
	digits = strings.TrimLeft(text, "0")
	if digits == "" {
		return "0", new(big.Int)
	}
	trimmed := strings.TrimRight(digits, "0")
	exponent.Add(exponent, big.NewInt(int64(len(digits)-len(trimmed))))
	digits = trimmed
	if negative {
		digits = "-" + digits
	}
	return digits, exponent
}

func decimalWord(n json.Number) string {
	digits, exponent := decimalParts(n)
	return digits + "e" + exponent.String()
}

// decimalAtLeast compares a JSON number with a positive decimal threshold.
func decimalAtLeast(n, threshold json.Number) bool {
	digits, exponent := decimalParts(n)
	if digits == "0" || digits[0] == '-' {
		return false
	}
	limit, limitExponent := decimalParts(threshold)
	exponent.Add(exponent, big.NewInt(int64(len(digits))))
	limitExponent.Add(limitExponent, big.NewInt(int64(len(limit))))
	if cmp := exponent.Cmp(limitExponent); cmp != 0 {
		return cmp > 0
	}
	for i := 0; i < len(digits) || i < len(limit); i++ {
		a, b := byte('0'), byte('0')
		if i < len(digits) {
			a = digits[i]
		}
		if i < len(limit) {
			b = limit[i]
		}
		if a != b {
			return a > b
		}
	}
	return true
}

func fakeResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
