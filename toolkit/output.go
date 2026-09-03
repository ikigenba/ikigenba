package toolkit

import (
	"context"
	"fmt"
)

const maxOutputChars = 30000

func truncateOutput(text string) string {
	runes := []rune(text)
	if len(runes) <= maxOutputChars {
		return text
	}
	return string(runes[:maxOutputChars]) + "\n" +
		fmt.Sprintf("[output truncated: showing %d of %d characters]", maxOutputChars, len(runes))
}

func capOutput[In any](fn func(context.Context, In) (string, error)) func(context.Context, In) (string, error) {
	return func(ctx context.Context, in In) (string, error) {
		text, err := fn(ctx, in)
		if err != nil {
			return text, err
		}
		return truncateOutput(text), nil
	}
}
