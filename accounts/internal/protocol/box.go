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
			versionOK = decodeBoxString(value, len("2.0")) == "2.0"
		case `"id"`:
			idOK = value[0] == '"' ||
				((value[0] == '-' || value[0] >= '0' && value[0] <= '9') && !bytes.ContainsAny(value, ".eE"))
		case `"method"`:
			method = decodeBoxString(value, len(MethodToolsList))
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
	if versionOK && idOK && (method == MethodToolsCall || method == MethodToolsList) {
		return method
	}
	return ""
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
