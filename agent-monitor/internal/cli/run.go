package cli

import (
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const hexDigits = "0123456789abcdef"

// Run handles one command invocation and returns its process exit code.
func Run(args []string, stdout, stderr io.Writer) ExitCode {
	if len(args) == 0 {
		return writeProduct(stdout, stderr, "hello, world\n")
	}

	arg := args[0]
	switch arg {
	case "--help", "-h":
		return writeProduct(stdout, stderr, Usage)
	case "--version", "-V":
		return writeProduct(stdout, stderr, Version+"\n")
	}

	kind := "unknown command"
	if strings.HasPrefix(arg, "-") {
		kind = "unknown option"
	}
	diagnostic := "agent-monitor: " + kind + " '" + escapeArgument(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"
	_, _ = stderr.Write([]byte(diagnostic))
	return ExitUsage
}

func writeProduct(stdout, stderr io.Writer, product string) ExitCode {
	_, err := stdout.Write([]byte(product))
	if err != nil {
		_, _ = stderr.Write([]byte("agent-monitor: write error: " + err.Error() + "\n"))
		return ExitWriteFailed
	}
	return ExitSuccess
}

func escapeArgument(arg string) string {
	var escaped strings.Builder
	for len(arg) > 0 {
		r, width := utf8.DecodeRuneInString(arg)
		first := arg[0]
		switch {
		case r == utf8.RuneError && width == 1:
			escaped.WriteString("\\x")
			escaped.WriteByte(hexDigits[first>>4])
			escaped.WriteByte(hexDigits[first&0xf])
		case first == '\\':
			escaped.WriteString("\\\\")
		case first == '\'':
			escaped.WriteString("\\'")
		case first == '\n':
			escaped.WriteString("\\n")
		case first == '\t':
			escaped.WriteString("\\t")
		case first == '\r':
			escaped.WriteString("\\r")
		case first < 0x20 || first == 0x7f:
			escaped.WriteString("\\x")
			escaped.WriteByte(hexDigits[first>>4])
			escaped.WriteByte(hexDigits[first&0xf])
		case first < utf8.RuneSelf || unicode.IsPrint(r):
			escaped.WriteString(arg[:width])
		case r <= 0xffff:
			escaped.WriteString("\\u")
			writeHexRune(&escaped, r, 4)
		default:
			escaped.WriteString("\\U")
			writeHexRune(&escaped, r, 8)
		}
		arg = arg[width:]
	}
	return escaped.String()
}

func writeHexRune(dst *strings.Builder, r rune, digits int) {
	for shift := (digits - 1) * 4; shift >= 0; shift -= 4 {
		dst.WriteByte(hexDigits[(r>>shift)&0xf])
	}
}
