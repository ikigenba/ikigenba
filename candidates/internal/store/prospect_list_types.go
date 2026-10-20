package store

import "time"

// ProspectListFilters selects the candidates admitted to a prospectList.
type ProspectListFilters struct {
	RevenueMin         *int64   `json:"revenue_min,omitempty"`
	RevenueMax         *int64   `json:"revenue_max,omitempty"`
	RevenueUnknown     bool     `json:"revenue_unknown,omitempty"`
	MaxMinutes         *int64   `json:"max_minutes,omitempty"`
	MinutesUnknown     bool     `json:"minutes_unknown,omitempty"`
	Categories         []string `json:"categories,omitempty"`
	CategoryUnknown    bool     `json:"category_unknown,omitempty"`
	Fleet              string   `json:"fleet,omitempty"`
	FleetUnknown       bool     `json:"fleet_unknown,omitempty"`
	Website            []string `json:"website,omitempty"`
	WebsiteUnknown     bool     `json:"website_unknown,omitempty"`
	Government         *bool    `json:"government,omitempty"`
	MaxFilingAgeDays   *int64   `json:"max_filing_age_days,omitempty"`
	FilingUnknown      bool     `json:"filing_unknown,omitempty"`
	MinCampaignAgeDays *int64   `json:"min_campaign_age_days,omitempty"`
	Class              string   `json:"class,omitempty"`
	ClassUnknown       bool     `json:"class_unknown,omitempty"`
	ShowExcluded       bool     `json:"show_excluded,omitempty"`
}

// ProspectListOrder is one term of a prospectList's ordering.
type ProspectListOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// ProspectList records a saved search and its authors.
type ProspectList struct {
	ID, Name                        string
	Size                            int64
	Filters                         ProspectListFilters
	Order                           []ProspectListOrder
	CreatedUserID, CreatedUserEmail string
	Created                         time.Time
	ChangedUserID, ChangedUserEmail string
	Changed                         time.Time
}

// ProspectListEdit replaces each part whose pointer is non-nil.
type ProspectListEdit struct {
	ID                string
	Name              *string
	Size              *int64
	Filters           *ProspectListFilters
	Order             *[]ProspectListOrder
	UserID, UserEmail string
}

// Reject is an immutable record of a prospectList-specific ruling.
type Reject struct {
	Kind, ProspectList, ProspectListName, Candidate, Why, UserID, UserEmail string
	At                                                                      time.Time
}

// ProspectListRead includes the search's present membership and standing rejects.
type ProspectListRead struct {
	ProspectList ProspectList
	Matching     int64
	Classified   int64
	Members      []Ranked
	Rejects      []Reject
}

// ProspectList vocabulary and bounds.
const (
	ProspectListIDPrefix     = "pl_"
	LegacyListIDPrefix       = "coh_"
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
	ProspectListSizeMin      = 1
	ProspectListSizeMax      = 100
	ProspectListSizeDefault  = 30
)

// MapListID maps a previous release list identifier.
func MapListID(s string) string {
	if validID(s, LegacyListIDPrefix) {
		return ProspectListIDPrefix + s[len(LegacyListIDPrefix):]
	}
	return s
}
