package wiki

import "wiki/internal/page"

func decodeCursor(token string, wantParts int) ([]string, error) {
	if token == "" {
		return nil, nil
	}
	parts, ok := page.DecodeCursor(token)
	if !ok || len(parts) != wantParts {
		return nil, ErrInvalidCursor
	}
	return parts, nil
}

func pageJobs(jobs []Job, limit int) []Job {
	if len(jobs) > limit {
		return jobs[:limit]
	}
	return jobs
}

func nextJobCursor(jobs []Job, limit int) string {
	if len(jobs) <= limit {
		return ""
	}
	last := jobs[limit-1]
	return page.EncodeCursor(formatTime(last.ReceivedAt), last.ID)
}

func pageSubjects(subjects []Subject, limit int) []Subject {
	if len(subjects) > limit {
		return subjects[:limit]
	}
	return subjects
}

func nextSubjectCursor(subjects []Subject, limit int) string {
	if len(subjects) <= limit {
		return ""
	}
	last := subjects[limit-1]
	return page.EncodeCursor(last.Name, last.ID)
}

func pageClaims(claims []Claim, limit int) []Claim {
	if len(claims) > limit {
		return claims[:limit]
	}
	return claims
}

func nextClaimCursor(claims []Claim, limit int) string {
	if len(claims) <= limit {
		return ""
	}
	return page.EncodeCursor(claims[limit-1].ID)
}

func pageCalls(calls []CallRecord, limit int) []CallRecord {
	if len(calls) > limit {
		return calls[:limit]
	}
	return calls
}

func nextCallCursor(calls []CallRecord, limit int) string {
	if len(calls) <= limit {
		return ""
	}
	last := calls[limit-1]
	return page.EncodeCursor(formatTime(last.StartedAt), last.ID)
}
