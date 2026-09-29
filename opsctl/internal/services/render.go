package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type entry struct {
	Name    string
	URL     string
	Icon    string
	Enabled bool
}

// render obtains every launcher entry before producing the file's exact bytes.
func render(ctx context.Context, env host.Env, hostName string) ([]byte, []entry, error) {
	if hostName == "" {
		return nil, nil, errors.New("host.name not set")
	}
	services, err := apps.Discover(env.Root)
	if err != nil {
		return nil, nil, err
	}
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("open host root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()

	entries := make([]entry, 0, len(services))
	for _, service := range services {
		if service.ManifestError != nil {
			return nil, nil, fmt.Errorf("%s: %w", service.Name, service.ManifestError)
		}
		if service.Manifest == nil || service.Manifest.App == "" {
			continue
		}
		base := path.Join("opt", service.Name)
		binary, err := filesystem.Lstat(path.Join(base, "bin", service.Name))
		if errors.Is(err, os.ErrNotExist) || err == nil && !binary.Mode().IsRegular() {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: bin/%s: %w", service.Name, service.Name, err)
		}
		iconPath := path.Join(base, apps.IconPath)
		iconInfo, err := filesystem.Lstat(iconPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %s: %w", service.Name, apps.IconPath, err)
		}
		if !iconInfo.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("%s: %s is not a regular file", service.Name, apps.IconPath)
		}
		icon, err := filesystem.ReadFile(iconPath)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %s: %w", service.Name, apps.IconPath, err)
		}
		if !utf8.Valid(icon) {
			return nil, nil, fmt.Errorf("%s: %s is not valid UTF-8", service.Name, apps.IconPath)
		}
		disabled, err := apps.Disabled(ctx, env, service.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", service.Name, err)
		}
		entries = append(entries, entry{
			Name: service.Name, URL: "https://" + service.Name + "." + hostName,
			Icon: string(icon), Enabled: !disabled,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return encodeEntries(entries), entries, nil
}

func encodeEntries(entries []entry) []byte {
	var output bytes.Buffer
	output.WriteString("{\n  \"services\": [")
	for i, item := range entries {
		if i == 0 {
			output.WriteByte('\n')
		} else {
			output.WriteString(",\n")
		}
		output.WriteString("    { \"name\": ")
		writeJSONString(&output, item.Name)
		output.WriteString(", \"url\": ")
		writeJSONString(&output, item.URL)
		output.WriteString(", \"icon\": ")
		writeJSONString(&output, item.Icon)
		output.WriteString(", \"enabled\": ")
		if item.Enabled {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
		output.WriteString(" }")
	}
	if len(entries) != 0 {
		output.WriteByte('\n')
		output.WriteString("  ")
	}
	output.WriteString("]\n}\n")
	return output.Bytes()
}

func writeJSONString(output *bytes.Buffer, value string) {
	const digits = "0123456789abcdef"
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		case '\u2028':
			output.WriteString(`\u2028`)
		case '\u2029':
			output.WriteString(`\u2029`)
		default:
			if character < 0x20 {
				output.WriteString(`\u00`)
				output.WriteByte(digits[character>>4])
				output.WriteByte(digits[character&15])
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
}
