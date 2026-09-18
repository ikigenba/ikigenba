package build

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/ikigenba/ikigenba/devctl/internal/appref"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

type preparedBuild struct {
	app     checkout.App
	version string
}

func run(ctx context.Context, name string, stdout io.Writer, deps seam.Deps) error {
	prepared, err := prepareBuild(ctx, name, deps)
	if err != nil {
		return err
	}
	return runPrepared(ctx, prepared, stdout, deps)
}

func prepareBuild(ctx context.Context, name string, deps seam.Deps) (preparedBuild, error) {
	if !appref.ValidName(name) {
		return preparedBuild{}, &UsageError{
			Message: fmt.Sprintf("'%s' is not a usable app name", name),
		}
	}

	opened, err := checkout.Open(ctx, deps)
	if err != nil {
		return preparedBuild{}, err
	}
	app, err := opened.App(name)
	if err != nil {
		return preparedBuild{}, err
	}
	clean, err := opened.Clean(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	if !clean {
		return preparedBuild{}, &UsageError{
			Message: "the working tree has uncommitted changes; commit them first",
		}
	}

	head, err := opened.Head(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	tags, err := opened.TagsAtHead(ctx)
	if err != nil {
		return preparedBuild{}, err
	}
	sort.Strings(tags)
	for _, tag := range tags {
		if version, ok := appref.VersionForTag(name, tag); ok {
			return preparedBuild{app: app, version: version}, nil
		}
	}

	return preparedBuild{}, &UsageError{
		Message: fmt.Sprintf("no tag %s/v<semver> points at HEAD (%s)", name, head),
	}
}

// runPrepared is the boundary between checkout validation and artifact work.
// Later build phases replace this body while retaining preparedBuild as input.
func runPrepared(_ context.Context, _ preparedBuild, _ io.Writer, _ seam.Deps) error {
	return nil
}
