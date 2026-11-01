package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func runConfigure(_ context.Context, args []string, stdin io.Reader, _ io.Writer, stderr io.Writer, deps seam.Deps) int {
	var space, token string
	var givenSpace, givenToken, missingToken bool
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--token":
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
				missingToken = true
				continue
			}
			i++
			token = args[i]
			givenToken = true
		case strings.HasPrefix(args[i], "--token="):
			token = strings.TrimPrefix(args[i], "--token=")
			givenToken = true
		default:
			space = args[i]
			givenSpace = true
		}
	}
	if missingToken {
		return usageError(stderr, "configure", "option '--token' needs a value")
	}
	if givenSpace && !config.ValidSpace(space) {
		return diagnosticCode(stderr, fmt.Sprintf("'%s' is not a space name", space), "", ExitUsage)
	}
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		return diagnostic(stderr, err.Error(), "")
	}
	if !givenSpace {
		host, loadErr := config.Load(paths.Paths{HostConfig: p.HostConfig})
		if loadErr != nil {
			return diagnostic(stderr, "could not read the configuration", loadErr.Error())
		}
		if host.HostSpace == "" {
			return usageError(stderr, "configure", "no space given")
		}
	}
	if !givenToken {
		token, err = readToken(stdin)
		if err != nil {
			return diagnostic(stderr, "could not read the token from stdin", err.Error())
		}
	}
	token = strings.TrimSuffix(strings.TrimSuffix(token, "\n"), "\r")
	if strings.TrimSpace(token) == "" {
		return diagnosticCode(stderr, "no token given", "", ExitUsage)
	}
	for _, b := range []byte(token) {
		if b < 0x20 || b == 0x7f {
			return diagnosticCode(stderr, "the token holds a control character", "", ExitUsage)
		}
	}
	if err = os.MkdirAll(p.Config, 0700); err != nil {
		return diagnostic(stderr, fmt.Sprintf("could not create '%s'", p.Config), err.Error())
	}
	tokenPath := filepath.Join(p.Config, paths.TokenFile)
	if err = replaceFile(tokenPath, []byte(token), 0600); err != nil {
		return diagnostic(stderr, fmt.Sprintf("could not write '%s'", tokenPath), err.Error())
	}
	if givenSpace {
		configPath := filepath.Join(p.Config, paths.ConfigFile)
		if err = replaceFile(configPath, []byte("space="+space+"\n"), 0600); err != nil {
			return diagnostic(stderr, fmt.Sprintf("could not write '%s'", configPath), err.Error())
		}
	}
	return ExitOK
}

func readToken(stdin io.Reader) (string, error) {
	token, err := bufio.NewReader(stdin).ReadString('\n')
	if err == io.EOF {
		err = nil
	}
	return token, err
}

func replaceFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".turns-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
