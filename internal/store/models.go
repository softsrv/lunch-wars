package store

import "time"

// Election status values. See schema.sql for the lifecycle.
const (
	StatusPending   = "pending"
	StatusJoin      = "join"
	StatusNominate  = "nominate"
	StatusVeto      = "veto"
	StatusVote      = "vote"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// MinFinalChoices is the minimum number of un-vetoed restaurants required
// before voting can open.
const MinFinalChoices = 3

// MaxNominationsPerUser is how many restaurants a single participant may
// nominate in one election.
const MaxNominationsPerUser = 3

// MaxVotesPerUser is how many ranked choices a single participant may cast.
const MaxVotesPerUser = 3

// Election is one lunch-decision run in a channel.
type Election struct {
	ID                 int64
	GuildID            *string
	ChannelID          string
	CreatedBy          string
	Status             string
	ScheduledAt        *time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	WinnerRestaurantID *int64
	ResultSummary      *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Restaurant is a nominated option within one election.
type Restaurant struct {
	ID             int64
	ElectionID     int64
	NameNormalized string
	DisplayName    string
	Vetoed         bool
	CreatedAt      time.Time
}

// Vote is one ranked choice cast by a participant.
type Vote struct {
	UserID       string
	RestaurantID int64
	Rank         int
}
