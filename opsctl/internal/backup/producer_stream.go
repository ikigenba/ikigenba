package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type archiveProducer func(context.Context, host.Env, io.Writer) error

// The two pipes impose backpressure in both directions: files cannot be read
// ahead of zstd, and zstd cannot deliver output ahead of the upload reader.
func uploadHostArchive(ctx context.Context, env host.Env, client cloud.Client, object, service string, archive archiveProducer) (int64, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tarReader, tarWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	closePipes := func(err error) {
		_ = tarReader.CloseWithError(err)
		_ = tarWriter.CloseWithError(err)
		_ = outputReader.CloseWithError(err)
		_ = outputWriter.CloseWithError(err)
	}
	stop := context.AfterFunc(runCtx, func() { closePipes(runCtx.Err()) })
	defer stop()

	tarDone := make(chan struct{})
	var tarErr error
	input := &archiveOutput{writer: tarWriter, started: make(chan struct{})}
	go func() {
		tarErr = archive(runCtx, env, input)
		_ = tarWriter.CloseWithError(tarErr)
		close(tarDone)
	}()
	select {
	case <-input.started:
	case <-tarDone:
		select {
		case <-input.started:
		default:
			closePipes(tarErr)
			return 0, tarErr
		}
	}
	output := &archiveOutput{writer: outputWriter, started: make(chan struct{})}
	done := make(chan struct{})
	var archiveErr error
	go func() {
		result, executeErr := env.Execute(runCtx, host.Command{
			Name: "zstd", Args: []string{"--quiet", "--stdout"},
			Stdin: tarReader, Stdout: output,
		})
		// A process that stops reading must also release a blocked tar writer.
		_ = tarReader.Close()
		<-tarDone
		label := "compress " + strconv.Quote(service) + " archive"
		switch {
		case ctx.Err() != nil:
			archiveErr = ctx.Err()
		case tarErr != nil && !errors.Is(tarErr, io.ErrClosedPipe):
			archiveErr = tarErr
		case executeErr != nil || result.ExitCode != 0:
			archiveErr = &host.CommandError{Label: label, Result: result, Err: executeErr}
		case tarErr != nil:
			archiveErr = tarErr
		case !bytes.Equal(output.magic, []byte{0x28, 0xb5, 0x2f, 0xfd}):
			archiveErr = fmt.Errorf("%s: invalid zstd output", label)
		}
		close(done)
		// EOF is the commit signal: it follows both successful producers.
		_ = outputWriter.CloseWithError(archiveErr)
	}()

	select {
	case <-output.started:
	case <-done:
		// A successful process with no output still fails magic validation.
		select {
		case <-output.started:
		default:
			return 0, archiveErr
		}
	}
	body := &archiveUploadBody{reader: outputReader}
	uploadErr := client.PutObject(ctx, object, body)
	// An uploader may refuse the object before reading any body. Release every
	// pipe before joining, so neither producer remains blocked on that reader.
	if uploadErr != nil {
		cancel()
		closePipes(uploadErr)
	}
	<-done
	_ = outputReader.Close()
	if body.failure != nil && archiveErr != nil {
		return body.size, archiveErr
	}
	if uploadErr != nil {
		return body.size, fmt.Errorf("upload %q: %w", object, uploadErr)
	}
	if archiveErr != nil {
		return body.size, archiveErr
	}
	return body.size, ctx.Err()
}

type archiveOutput struct {
	writer  io.Writer
	started chan struct{}
	once    sync.Once
	magic   []byte
}

func (w *archiveOutput) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.once.Do(func() { close(w.started) })
	if len(w.magic) < 4 {
		w.magic = append(w.magic, p[:min(4-len(w.magic), len(p))]...)
	}
	return w.writer.Write(p)
}

type archiveUploadBody struct {
	reader  io.Reader
	size    int64
	failure error
}

func (r *archiveUploadBody) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.size += int64(n)
	if err != nil && err != io.EOF {
		r.failure = err
	}
	return n, err
}
