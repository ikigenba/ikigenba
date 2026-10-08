package cli

import (
	"fmt"
	"io"
	"strings"
)

func writeStepReport(output io.Writer, step, detail string, success bool) error {
	detail = safeReportDetail(detail)
	if success {
		_, err := fmt.Fprintf(output, "%s: ok (%s)\n", step, detail)
		return err
	}
	_, err := fmt.Fprintf(output, "%s: failed: %s\n", step, detail)
	return err
}

func safeReportDetail(detail string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(detail)
}
