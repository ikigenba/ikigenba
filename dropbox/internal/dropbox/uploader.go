package dropbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	uploaderPollInterval    = time.Second
	uploaderPoisonThreshold = 5
	uploaderBackoffBase     = time.Second
	uploaderBackoffMax      = time.Minute
)

// RunUploader drains due durable writes and waits for future retry gates. It
// exits cleanly when its service context is cancelled.
func (s *Service) RunUploader(ctx context.Context) error {
	if s.DB == nil || s.Store == nil || s.Client == nil || s.Mirror == nil {
		return fmt.Errorf("uploader: service is not fully configured")
	}
	ticker := time.NewTicker(uploaderPollInterval)
	defer ticker.Stop()
	for {
		if err := s.drainUploads(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// drainUploads processes every request due at the current clock instant. A
// per-row API failure is recorded durably and does not prevent later rows from
// draining; database faults remain structural errors.
func (s *Service) drainUploads(ctx context.Context) error {
	now := s.nowTime()
	var rows []UploadQueueRow
	if err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		rows, err = s.Store.DueUploads(tx, now.Format(time.RFC3339Nano))
		return err
	}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.uploadRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) uploadRow(ctx context.Context, row UploadQueueRow) error {
	var rev string
	var err error
	switch row.Op {
	case "put":
		file, info, openErr := s.Mirror.Open(row.Path)
		if openErr == nil {
			rev, err = s.Client.Upload(ctx, row.Path, file, info.Size())
			_ = file.Close()
		} else {
			err = openErr
		}
	case "mkdir":
		err = s.Client.CreateFolder(ctx, row.Path)
	case "delete":
		err = s.Client.DeletePath(ctx, row.Path)
	case "move":
		if !row.Dest.Valid {
			err = fmt.Errorf("uploader: move %q has no destination", row.Path)
		} else {
			err = s.Client.Move(ctx, row.Path, row.Dest.String)
		}
	default:
		err = fmt.Errorf("uploader: unknown operation %q", row.Op)
	}
	if err != nil {
		return s.recordUploadFailure(ctx, row, err)
	}

	return s.inTx(ctx, func(tx *sql.Tx) error {
		if row.Op == "put" {
			current, getErr := s.Store.GetFile(tx, row.Path)
			if getErr != nil {
				return fmt.Errorf("uploader: get uploaded file: %w", getErr)
			}
			if err := s.Store.UpsertFile(tx, current.Path, rev, current.ContentHash, current.Size, s.nowTime().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return s.Store.ClearUpload(tx, row.Path)
	})
}

func (s *Service) recordUploadFailure(ctx context.Context, row UploadQueueRow, cause error) error {
	attempts := row.Attempts + 1
	backoff := uploaderBackoffBase << min(attempts-1, 6)
	if backoff > uploaderBackoffMax {
		backoff = uploaderBackoffMax
	}
	next := s.nowTime().Add(backoff).Format(time.RFC3339Nano)
	return s.inTx(ctx, func(tx *sql.Tx) error {
		return s.Store.FailUpload(tx, row.Path, cause.Error(), next, attempts >= uploaderPoisonThreshold)
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
