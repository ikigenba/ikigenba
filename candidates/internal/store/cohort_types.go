package store

import "time"

// CohortFilters selects the candidates admitted to a cohort.
type CohortFilters struct {
	RevenueMin       *int64   `json:"revenue_min,omitempty"`
	RevenueMax       *int64   `json:"revenue_max,omitempty"`
	RevenueUnknown   bool     `json:"revenue_unknown,omitempty"`
	MaxMinutes       *int64   `json:"max_minutes,omitempty"`
	MinutesUnknown   bool     `json:"minutes_unknown,omitempty"`
	Categories       []string `json:"categories,omitempty"`
	CategoryUnknown  bool     `json:"category_unknown,omitempty"`
	Fleet            string   `json:"fleet,omitempty"`
	FleetUnknown     bool     `json:"fleet_unknown,omitempty"`
	Website          []string `json:"website,omitempty"`
	WebsiteUnknown   bool     `json:"website_unknown,omitempty"`
	Government       *bool    `json:"government,omitempty"`
	MaxFilingAgeDays *int64   `json:"max_filing_age_days,omitempty"`
	FilingUnknown    bool     `json:"filing_unknown,omitempty"`
	Class            string   `json:"class,omitempty"`
	ClassUnknown     bool     `json:"class_unknown,omitempty"`
	ShowExcluded     bool     `json:"show_excluded,omitempty"`
}

// CohortOrder is one term of a cohort's ordering.
type CohortOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// Cohort records a saved search and its authors.
type Cohort struct {
	ID, Name                        string
	Size                            int64
	Filters                         CohortFilters
	Order                           []CohortOrder
	CreatedUserID, CreatedUserEmail string
	Created                         time.Time
	ChangedUserID, ChangedUserEmail string
	Changed                         time.Time
}

// CohortEdit replaces each part whose pointer is non-nil.
type CohortEdit struct {
	ID                string
	Name              *string
	Size              *int64
	Filters           *CohortFilters
	Order             *[]CohortOrder
	UserID, UserEmail string
}

// Reject is an immutable record of a cohort-specific ruling.
type Reject struct {
	Kind, Cohort, CohortName, Candidate, Why, UserID, UserEmail string
	At                                                          time.Time
}

// CohortRead includes the search's present membership and standing rejects.
type CohortRead struct {
	Cohort     Cohort
	Matching   int64
	Classified int64
	Members    []Ranked
	Rejects    []Reject
}

// Cohort vocabulary and bounds.
const (
	CohortIDPrefix           = "coh_"
	OrderOperatingConfidence = "operating_confidence"
	OrderRevenueLow          = "revenue_low"
	OrderRevenueConfidence   = "revenue_confidence"
	OrderDriveMinutes        = "drive_minutes"
	OrderNewestFiling        = "newest_filing"
	OrderKnownSince          = "known_since"
	DirectionDesc            = "desc"
	DirectionAsc             = "asc"
	KindReject               = "reject"
	KindUnreject             = "unreject"
	CohortSizeMin            = 1
	CohortSizeMax            = 100
	CohortSizeDefault        = 30
)
