package pages

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func datetime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func active(u store.Run) bool {
	return u.Status == store.StatusQueued || u.Status == store.StatusRunning
}
func duration(u store.Run) *Duration {
	if active(u) || u.Status == store.StatusFailed {
		return nil
	}
	s := int64(u.Finished.Sub(u.Started) / time.Second)
	return &Duration{Minutes: s / 60, Seconds: s % 60}
}
func size(n int64) Size {
	if n < 1000 {
		return Size{Number: strconv.FormatInt(n, 10), Unit: "byte"}
	}
	scale, unit := int64(1000), "kilobyte"
	if n >= 1000000 {
		scale = 1000000
		unit = "megabyte"
	}
	return Size{Number: fmt.Sprintf("%d.%d", n/scale, (n/(scale/10))%10), Unit: unit}
}
func count(n int64) string {
	s := strconv.FormatInt(n, 10)
	out := ""
	for i, c := range s {
		out += string(c)
		if remaining := len(s) - i - 1; remaining > 0 && remaining%3 == 0 {
			out += ","
		}
	}
	return out
}
func cost(u store.Run) string {
	if active(u) {
		return ""
	}
	n := u.Usage.CostNanos
	m := n / 1000
	if n%1000 >= 500 {
		m++
	}
	return fmt.Sprintf("%d.%06d", m/1000000, m%1000000)
}
func usage(u store.Run) *Usage {
	if active(u) {
		return nil
	}
	v := u.Usage
	return &Usage{Calls: count(v.Calls), ToolCalls: count(v.ToolCalls), InputTokens: count(v.InputTokens), CachedTokens: count(v.CachedTokens), OutputTokens: count(v.OutputTokens), ReasoningTokens: count(v.ReasoningTokens), Cost: cost(u)}
}
func runRow(u store.Run, name string) RunRow {
	return RunRow{ID: u.ID, URL: "/" + name + "/runs/" + u.ID + "/", Status: u.Status, ExitCode: u.ExitCode, Model: u.Model, Cost: cost(u), Started: u.Started.UTC().Format(MinuteLayout), StartedAt: datetime(u.Started), Duration: duration(u)}
}
