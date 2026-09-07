package agentkit

import (
	"path/filepath"
	"strings"
)

type accessKind uint8

const (
	accessNone accessKind = iota
	accessPaths
	accessAll
)

// Access describes which other calls a tool call blocks. Its representation is
// private so values can only be built by the constructors below.
type Access struct {
	kind  accessKind
	paths []string
}

// BlocksNone returns access that conflicts only with BlocksAll.
func BlocksNone() Access { return Access{} }

// BlocksPaths returns access that conflicts with overlapping cleaned paths.
func BlocksPaths(paths ...string) Access {
	if len(paths) == 0 {
		return BlocksNone()
	}
	cleaned := make([]string, len(paths))
	for index, path := range paths {
		cleaned[index] = filepath.Clean(path)
	}
	return Access{kind: accessPaths, paths: cleaned}
}

// BlocksAll returns access that conflicts with every other call.
func BlocksAll() Access { return Access{kind: accessAll} }

func accessesConflict(left, right Access) bool {
	if left.kind == accessAll || right.kind == accessAll {
		return true
	}
	if left.kind != accessPaths || right.kind != accessPaths {
		return false
	}
	for _, leftPath := range left.paths {
		for _, rightPath := range right.paths {
			if pathsOverlap(leftPath, rightPath) {
				return true
			}
		}
	}
	return false
}

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
