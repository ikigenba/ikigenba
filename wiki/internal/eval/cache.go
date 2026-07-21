package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

func NewDiskCache(dir, model string, next EmbedFunc) EmbedFunc {
	var mu sync.Mutex
	return func(ctx context.Context, texts []string) ([][]float32, error) {
		mu.Lock()
		defer mu.Unlock()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create embedding cache: %w", err)
		}
		vectors := make([][]float32, len(texts))
		missingTexts := make([]string, 0)
		missingIndexes := make([]int, 0)
		for i, value := range texts {
			path := cachePath(dir, model, value)
			data, err := os.ReadFile(path)
			if err == nil && json.Unmarshal(data, &vectors[i]) == nil {
				continue
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("read embedding cache: %w", err)
			}
			missingTexts = append(missingTexts, value)
			missingIndexes = append(missingIndexes, i)
		}
		if len(missingTexts) == 0 {
			return vectors, nil
		}
		fresh, err := next(ctx, missingTexts)
		if err != nil {
			return nil, err
		}
		if len(fresh) != len(missingTexts) {
			return nil, fmt.Errorf("embed returned %d vectors for %d texts", len(fresh), len(missingTexts))
		}
		for i, vector := range fresh {
			index := missingIndexes[i]
			vectors[index] = vector
			data, err := json.Marshal(vector)
			if err != nil {
				return nil, fmt.Errorf("marshal embedding cache: %w", err)
			}
			path := cachePath(dir, model, texts[index])
			temp, err := os.CreateTemp(dir, ".embedding-*")
			if err != nil {
				return nil, fmt.Errorf("create embedding cache temp: %w", err)
			}
			tempName := temp.Name()
			if _, err = temp.Write(data); err == nil {
				err = temp.Close()
			} else {
				_ = temp.Close()
			}
			if err == nil {
				err = os.Rename(tempName, path)
			}
			if err != nil {
				_ = os.Remove(tempName)
				return nil, fmt.Errorf("write embedding cache: %w", err)
			}
		}
		return vectors, nil
	}
}

func cachePath(dir, model, text string) string {
	sum := sha256.Sum256([]byte(model + "\x00" + text))
	return filepath.Join(dir, fmt.Sprintf("%x.json", sum))
}
