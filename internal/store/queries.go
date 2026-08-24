package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrConflict indicates a request violated a uniqueness rule (e.g. a second
// veto from the same user, or a duplicate active election in a channel).
var ErrConflict = errors.New("store: conflict")

// ErrNotFound indicates the requested row does not exist.
var ErrNotFound = errors.New("store: not found")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// UpsertUser records a Discord user's latest known username.
func (s *Store) UpsertUser(ctx context.Context, id, username string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, username) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET username = EXCLUDED.username, updated_at = now()
	`, id, username)
	if err != nil {
		return fmt.Errorf("store: upserting user: %w", err)
	}
	return nil
}

// CreateElectionParams describes a new election to create.
type CreateElectionParams struct {
	GuildID     *string
	ChannelID   string
	CreatedBy   string
	ScheduledAt *time.Time // nil means start immediately
}

func electionInitialStatus(p CreateElectionParams) string {
	if p.ScheduledAt != nil {
		return StatusPending
	}
	return StatusJoin
}

// CreateElection inserts a new election, returning ErrConflict if the
// channel already has a non-terminal election in progress.
func (s *Store) CreateElection(ctx context.Context, p CreateElectionParams) (Election, error) {
	status := electionInitialStatus(p)
	var startedAt *time.Time
	if status != StatusPending {
		now := time.Now().UTC()
		startedAt = &now
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO elections (guild_id, channel_id, created_by, status, scheduled_at, started_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+electionColumns+`
	`, p.GuildID, p.ChannelID, p.CreatedBy, status, p.ScheduledAt, startedAt)

	e, err := scanElection(row)
	if err != nil {
		if isUniqueViolation(err) {
			return Election{}, ErrConflict
		}
		return Election{}, fmt.Errorf("store: creating election: %w", err)
	}
	return e, nil
}

func scanElection(row pgx.Row) (Election, error) {
	var e Election
	err := row.Scan(&e.ID, &e.GuildID, &e.ChannelID, &e.CreatedBy, &e.Status,
		&e.ScheduledAt, &e.StartedAt, &e.CompletedAt, &e.WinnerRestaurantID, &e.ResultSummary,
		&e.CreatedAt, &e.UpdatedAt)
	return e, err
}

const electionColumns = `id, guild_id, channel_id, created_by, status, scheduled_at, started_at, completed_at, winner_restaurant_id, result_summary, created_at, updated_at`

// GetActiveElection returns the channel's current non-terminal election, if any.
func (s *Store) GetActiveElection(ctx context.Context, channelID string) (*Election, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+electionColumns+` FROM elections
		WHERE channel_id = $1 AND status NOT IN ('completed', 'cancelled')
	`, channelID)
	e, err := scanElection(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: getting active election: %w", err)
	}
	return &e, nil
}

// GetElection fetches an election by ID.
func (s *Store) GetElection(ctx context.Context, id int64) (Election, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+electionColumns+` FROM elections WHERE id = $1`, id)
	e, err := scanElection(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Election{}, ErrNotFound
		}
		return Election{}, fmt.Errorf("store: getting election: %w", err)
	}
	return e, nil
}

// ListDuePendingElections returns pending elections whose scheduled time has arrived.
func (s *Store) ListDuePendingElections(ctx context.Context, now time.Time) ([]Election, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+electionColumns+` FROM elections
		WHERE status = 'pending' AND scheduled_at <= $1
	`, now)
	if err != nil {
		return nil, fmt.Errorf("store: listing due elections: %w", err)
	}
	defer rows.Close()

	var out []Election
	for rows.Next() {
		e, err := scanElection(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scanning due election: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// StartElection transitions a pending scheduled election into its first
// active phase (join).
func (s *Store) StartElection(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE elections SET status = $2, started_at = now(), updated_at = now() WHERE id = $1
	`, id, StatusJoin)
	if err != nil {
		return fmt.Errorf("store: starting election: %w", err)
	}
	return nil
}

// SetElectionStatus transitions an election to a new phase.
func (s *Store) SetElectionStatus(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE elections SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("store: updating election status: %w", err)
	}
	return nil
}

// CompleteElection marks an election as finished with its winning restaurant
// and a human-readable summary of the tally.
func (s *Store) CompleteElection(ctx context.Context, id int64, winnerRestaurantID int64, summary string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE elections
		SET status = 'completed', completed_at = now(), updated_at = now(),
		    winner_restaurant_id = $2, result_summary = $3
		WHERE id = $1
	`, id, winnerRestaurantID, summary)
	if err != nil {
		return fmt.Errorf("store: completing election: %w", err)
	}
	return nil
}

// CancelElection marks an election as cancelled.
func (s *Store) CancelElection(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE elections SET status = 'cancelled', updated_at = now() WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("store: cancelling election: %w", err)
	}
	return nil
}

// AddParticipant records a user as joined. It is idempotent.
func (s *Store) AddParticipant(ctx context.Context, electionID int64, userID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO election_participants (election_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, electionID, userID)
	if err != nil {
		return fmt.Errorf("store: adding participant: %w", err)
	}
	return nil
}

// IsParticipant reports whether a user has joined the election.
func (s *Store) IsParticipant(ctx context.Context, electionID int64, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM election_participants WHERE election_id = $1 AND user_id = $2)
	`, electionID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: checking participant: %w", err)
	}
	return exists, nil
}

// ListParticipantIDs returns the user IDs who have joined the election, in
// join order.
func (s *Store) ListParticipantIDs(ctx context.Context, electionID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id FROM election_participants WHERE election_id = $1 ORDER BY joined_at
	`, electionID)
	if err != nil {
		return nil, fmt.Errorf("store: listing participants: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scanning participant: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CountParticipants returns how many users have joined the election.
func (s *Store) CountParticipants(ctx context.Context, electionID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM election_participants WHERE election_id = $1`, electionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting participants: %w", err)
	}
	return n, nil
}

func normalizeRestaurantName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// AddNomination records a participant's nomination of a restaurant, creating
// the restaurant row (deduplicated case-insensitively within the election)
// if it doesn't already exist. It returns ErrConflict if the user has
// already nominated that same restaurant.
func (s *Store) AddNomination(ctx context.Context, electionID int64, userID, restaurantName string) (Restaurant, error) {
	normalized := normalizeRestaurantName(restaurantName)
	if normalized == "" {
		return Restaurant{}, fmt.Errorf("store: empty restaurant name")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Restaurant{}, fmt.Errorf("store: beginning nomination tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var r Restaurant
	err = tx.QueryRow(ctx, `
		INSERT INTO restaurants (election_id, name_normalized, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (election_id, name_normalized) DO UPDATE SET name_normalized = EXCLUDED.name_normalized
		RETURNING id, election_id, name_normalized, display_name, vetoed, created_at
	`, electionID, normalized, strings.TrimSpace(restaurantName)).
		Scan(&r.ID, &r.ElectionID, &r.NameNormalized, &r.DisplayName, &r.Vetoed, &r.CreatedAt)
	if err != nil {
		return Restaurant{}, fmt.Errorf("store: upserting restaurant: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO nominations (election_id, user_id, restaurant_id) VALUES ($1, $2, $3)
	`, electionID, userID, r.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Restaurant{}, ErrConflict
		}
		return Restaurant{}, fmt.Errorf("store: inserting nomination: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Restaurant{}, fmt.Errorf("store: committing nomination tx: %w", err)
	}
	return r, nil
}

// CountNominationsByUser returns how many restaurants a user has nominated.
func (s *Store) CountNominationsByUser(ctx context.Context, electionID int64, userID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM nominations WHERE election_id = $1 AND user_id = $2
	`, electionID, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting nominations: %w", err)
	}
	return n, nil
}

// ListRestaurants returns every nominated restaurant in an election, ordered
// by ID (i.e. nomination order). When onlyActive is true, vetoed restaurants
// are excluded.
func (s *Store) ListRestaurants(ctx context.Context, electionID int64, onlyActive bool) ([]Restaurant, error) {
	query := `SELECT id, election_id, name_normalized, display_name, vetoed, created_at FROM restaurants WHERE election_id = $1`
	if onlyActive {
		query += ` AND vetoed = FALSE`
	}
	query += ` ORDER BY id`

	rows, err := s.pool.Query(ctx, query, electionID)
	if err != nil {
		return nil, fmt.Errorf("store: listing restaurants: %w", err)
	}
	defer rows.Close()

	var out []Restaurant
	for rows.Next() {
		var r Restaurant
		if err := rows.Scan(&r.ID, &r.ElectionID, &r.NameNormalized, &r.DisplayName, &r.Vetoed, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning restaurant: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HasVetoed reports whether a user has already cast their (single) veto.
func (s *Store) HasVetoed(ctx context.Context, electionID int64, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM vetoes WHERE election_id = $1 AND user_id = $2)
	`, electionID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: checking veto: %w", err)
	}
	return exists, nil
}

// CountActiveRestaurants returns how many un-vetoed restaurants remain.
func (s *Store) CountActiveRestaurants(ctx context.Context, electionID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM restaurants WHERE election_id = $1 AND vetoed = FALSE
	`, electionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting active restaurants: %w", err)
	}
	return n, nil
}

// AddVeto records a user's veto of a restaurant and marks it vetoed. It
// returns ErrConflict if the user has already used their veto.
func (s *Store) AddVeto(ctx context.Context, electionID int64, userID string, restaurantID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: beginning veto tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO vetoes (election_id, user_id, restaurant_id) VALUES ($1, $2, $3)
	`, electionID, userID, restaurantID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: inserting veto: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE restaurants SET vetoed = TRUE WHERE id = $1`, restaurantID)
	if err != nil {
		return fmt.Errorf("store: marking restaurant vetoed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: committing veto tx: %w", err)
	}
	return nil
}

// ReplaceVotes overwrites a user's ranked ballot for an election. Passing an
// empty slice clears their vote. restaurantIDs is in preference order
// (index 0 = 1st choice).
func (s *Store) ReplaceVotes(ctx context.Context, electionID int64, userID string, restaurantIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: beginning vote tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM votes WHERE election_id = $1 AND user_id = $2`, electionID, userID)
	if err != nil {
		return fmt.Errorf("store: clearing previous votes: %w", err)
	}

	for i, rid := range restaurantIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO votes (election_id, user_id, restaurant_id, rank) VALUES ($1, $2, $3, $4)
		`, electionID, userID, rid, i+1)
		if err != nil {
			return fmt.Errorf("store: inserting vote: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: committing vote tx: %w", err)
	}
	return nil
}

// ListVotes returns every cast ballot in an election.
func (s *Store) ListVotes(ctx context.Context, electionID int64) ([]Vote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, restaurant_id, rank FROM votes WHERE election_id = $1 ORDER BY user_id, rank
	`, electionID)
	if err != nil {
		return nil, fmt.Errorf("store: listing votes: %w", err)
	}
	defer rows.Close()

	var out []Vote
	for rows.Next() {
		var v Vote
		if err := rows.Scan(&v.UserID, &v.RestaurantID, &v.Rank); err != nil {
			return nil, fmt.Errorf("store: scanning vote: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CountVoters returns how many distinct users have cast a ballot.
func (s *Store) CountVoters(ctx context.Context, electionID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT user_id) FROM votes WHERE election_id = $1
	`, electionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: counting voters: %w", err)
	}
	return n, nil
}
