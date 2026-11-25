package protocol

import (
	"bytes"
	"encoding/json"
)

const jsonSpace = " \t\r\n"

// BoxMethod returns the method of a request that can cross the box endpoint.
func BoxMethod(body []byte) string {
	body = bytes.Trim(body, jsonSpace)
	if len(body) == 0 || body[0] != '{' || !json.Valid(body) {
		return ""
	}
	var method string
	var versionOK, idOK bool
	// The JSON scanner has already checked the grammar. Walk slices of the
	// same buffer so large argument values need no second copy.
	for rest := bytes.TrimLeft(body[1:], jsonSpace); rest[0] != '}'; {
		nameEnd := jsonStringEnd(rest)
		name := rest[:nameEnd]
		colon := bytes.TrimLeft(rest[nameEnd:], jsonSpace)
		rest = bytes.TrimLeft(colon[1:], jsonSpace)
		end := jsonValueEnd(rest)
		value := rest[:end]
		switch string(name) {
		case `"jsonrpc"`:
			if versionOK || decodeBoxString(value, len("2.0")) != "2.0" {
				return ""
			}
			versionOK = true
		case `"id"`:
			if idOK || !BoxIDValid(value) {
				return ""
			}
			idOK = true
		case `"method"`:
			if method != "" {
				return ""
			}
			method = decodeBoxString(value, len(MethodToolsList))
			if method != MethodToolsCall && method != MethodToolsList {
				return ""
			}
		default:
			// The downstream RPC decoder resolves escaped names before
			// selecting the last value. Keep aliases from shadowing the
			// envelope selected by this literal-name check.
			switch decodeBoxString(name, len("jsonrpc")) {
			case "jsonrpc", "id", "method":
				return ""
			}
		}
		rest = bytes.TrimLeft(rest[end:], jsonSpace)
		if rest[0] == ',' {
			rest = bytes.TrimLeft(rest[1:], jsonSpace)
		}
	}
	if versionOK && idOK {
		return method
	}
	return ""
}

// BoxIDValid reports whether a syntactically valid JSON value is a box request id.
func BoxIDValid(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	first := raw[0]
	return first == '"' || (first == '-' || first >= '0' && first <= '9') && !bytes.ContainsAny(raw, ".eE")
}

func decodeBoxString(raw []byte, maxDecoded int) string {
	// Every ASCII byte in a matching string needs at most six JSON bytes.
	if len(raw) > 2+6*maxDecoded || raw[0] != '"' {
		return ""
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	return decoded
}

func jsonStringEnd(raw []byte) int {
	for i := 1; ; i++ {
		switch raw[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
}

func jsonValueEnd(raw []byte) int {
	switch raw[0] {
	case '"':
		return jsonStringEnd(raw)
	case '{', '[':
		depth := 1
		for i := 1; ; i++ {
			switch raw[i] {
			case '"':
				i += jsonStringEnd(raw[i:]) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	default:
		return bytes.IndexAny(raw, ",}"+jsonSpace)
	}
}
