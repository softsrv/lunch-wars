package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestMain-less setup: these tests exercise the store against a real
// Postgres instance. They're skipped unless MUNCHBOT_TEST_DATABASE_URL is
// set, so `go test ./...` stays hermetic by default. Point it at a
// throwaway database, e.g.:
//
//	docker run -d -e POSTGRES_PASSWORD=munchbot -e POSTGRES_DB=munchbot -p 55432:5432 postgres:16-alpine
//	MUNCHBOT_TEST_DATABASE_URL="postgres://postgres:munchbot@localhost:55432/munchbot?sslmode=disable" go test ./internal/store/... -v

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MUNCHBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MUNCHBOT_TEST_DATABASE_URL not set; skipping Postgres integration tests")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() {
		// Leave a clean slate for the next test run.
		_, _ = s.pool.Exec(ctx, `TRUNCATE elections, users RESTART IDENTITY CASCADE`)
		s.Close()
	})
	return s
}

func mustUser(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.UpsertUser(context.Background(), id, "user-"+id); err != nil {
		t.Fatalf("upserting user %s: %v", id, err)
	}
}

func TestIntegration_FullElectionLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	host, u2, u3 := "host", "u2", "u3"
	for _, u := range []string{host, u2, u3} {
		mustUser(t, s, u)
	}

	e, err := s.CreateElection(ctx, CreateElectionParams{
		ChannelID: "chan-1",
		CreatedBy: host,
	})
	if err != nil {
		t.Fatalf("CreateElection: %v", err)
	}
	if e.Status != StatusJoin {
		t.Fatalf("expected initial status %q, got %q", StatusJoin, e.Status)
	}

	// A second election in the same channel should conflict.
	if _, err := s.CreateElection(ctx, CreateElectionParams{ChannelID: "chan-1", CreatedBy: host}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate active election, got %v", err)
	}

	for _, u := range []string{host, u2, u3} {
		if err := s.AddParticipant(ctx, e.ID, u); err != nil {
			t.Fatalf("AddParticipant(%s): %v", u, err)
		}
	}
	n, err := s.CountParticipants(ctx, e.ID)
	if err != nil || n != 3 {
		t.Fatalf("CountParticipants = %d, %v; want 3, nil", n, err)
	}

	if err := s.SetElectionStatus(ctx, e.ID, StatusNominate); err != nil {
		t.Fatalf("SetElectionStatus(nominate): %v", err)
	}

	// Nominations, including a case-insensitive duplicate across users.
	names := []struct {
		user, name string
	}{
		{host, "Situ"}, {host, "Cracklemi"}, {u2, "MarketPho"}, {u3, "situ"},
	}
	var situID int64
	for _, n := range names {
		r, err := s.AddNomination(ctx, e.ID, n.user, n.name)
		if err != nil {
			t.Fatalf("AddNomination(%s, %s): %v", n.user, n.name, err)
		}
		if n.name == "Situ" {
			situID = r.ID
		}
	}
	// u3 nominating "situ" (different casing) should have resolved to the
	// same restaurant as "Situ", not created a duplicate row.
	all, err := s.ListRestaurants(ctx, e.ID, false)
	if err != nil {
		t.Fatalf("ListRestaurants: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 deduplicated restaurants, got %d: %+v", len(all), all)
	}

	// Re-nominating the exact same restaurant by the same user should conflict.
	if _, err := s.AddNomination(ctx, e.ID, host, "Situ"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate nomination, got %v", err)
	}

	if err := s.SetElectionStatus(ctx, e.ID, StatusVeto); err != nil {
		t.Fatalf("SetElectionStatus(veto): %v", err)
	}

	// With exactly 3 restaurants, a veto must be rejected (would drop below
	// the minimum of 3).
	active, err := s.CountActiveRestaurants(ctx, e.ID)
	if err != nil || active != 3 {
		t.Fatalf("CountActiveRestaurants = %d, %v; want 3, nil", active, err)
	}

	// Add a 4th restaurant directly so a veto is legal, mirroring what the
	// bot layer would prevent via /next's minimum-nominee check but letting
	// us test the veto path here.
	r4, err := s.AddNomination(ctx, e.ID, u2, "FourthPlace")
	if err != nil {
		t.Fatalf("AddNomination(FourthPlace): %v", err)
	}

	if err := s.AddVeto(ctx, e.ID, host, r4.ID); err != nil {
		t.Fatalf("AddVeto: %v", err)
	}
	// A second veto by the same user must conflict.
	if err := s.AddVeto(ctx, e.ID, host, situID); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for second veto by same user, got %v", err)
	}

	remaining, err := s.ListRestaurants(ctx, e.ID, true)
	if err != nil || len(remaining) != 3 {
		t.Fatalf("ListRestaurants(active) = %d, %v; want 3, nil", len(remaining), err)
	}

	if err := s.SetElectionStatus(ctx, e.ID, StatusVote); err != nil {
		t.Fatalf("SetElectionStatus(vote): %v", err)
	}

	// Cast ranked ballots; host ranks Situ first.
	if err := s.ReplaceVotes(ctx, e.ID, host, []int64{situID}); err != nil {
		t.Fatalf("ReplaceVotes(host): %v", err)
	}
	// Overwrite with a fuller ballot to confirm ReplaceVotes clears stale rows.
	otherIDs := make([]int64, 0)
	for _, r := range remaining {
		if r.ID != situID {
			otherIDs = append(otherIDs, r.ID)
		}
	}
	if err := s.ReplaceVotes(ctx, e.ID, host, append([]int64{situID}, otherIDs...)); err != nil {
		t.Fatalf("ReplaceVotes(host, full ballot): %v", err)
	}

	votes, err := s.ListVotes(ctx, e.ID)
	if err != nil {
		t.Fatalf("ListVotes: %v", err)
	}
	hostVotes := 0
	for _, v := range votes {
		if v.UserID == host {
			hostVotes++
		}
	}
	if hostVotes != 3 {
		t.Fatalf("expected host's replaced ballot to have 3 entries, got %d", hostVotes)
	}

	if err := s.CompleteElection(ctx, e.ID, situID, "Situ wins!"); err != nil {
		t.Fatalf("CompleteElection: %v", err)
	}
	got, err := s.GetElection(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetElection: %v", err)
	}
	if got.Status != StatusCompleted || got.WinnerRestaurantID == nil || *got.WinnerRestaurantID != situID {
		t.Fatalf("unexpected completed election state: %+v", got)
	}

	// Now that it's terminal, a new election can be created in the same channel.
	if _, err := s.CreateElection(ctx, CreateElectionParams{ChannelID: "chan-1", CreatedBy: host}); err != nil {
		t.Fatalf("expected new election to succeed after completion, got %v", err)
	}
}

func TestIntegration_ScheduledElectionStartsPending(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustUser(t, s, "sched-host")

	future := time.Now().Add(time.Hour)
	e, err := s.CreateElection(ctx, CreateElectionParams{
		ChannelID:   "sched-chan",
		CreatedBy:   "sched-host",
		ScheduledAt: &future,
	})
	if err != nil {
		t.Fatalf("CreateElection: %v", err)
	}
	if e.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", e.Status)
	}

	due, err := s.ListDuePendingElections(ctx, time.Now())
	if err != nil {
		t.Fatalf("ListDuePendingElections: %v", err)
	}
	for _, d := range due {
		if d.ID == e.ID {
			t.Fatalf("election not yet due should not appear in due list")
		}
	}

	due, err = s.ListDuePendingElections(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListDuePendingElections: %v", err)
	}
	found := false
	for _, d := range due {
		if d.ID == e.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled election to be due 2h in the future")
	}

	if err := s.StartElection(ctx, e.ID); err != nil {
		t.Fatalf("StartElection: %v", err)
	}
	got, err := s.GetElection(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetElection: %v", err)
	}
	if got.Status != StatusJoin {
		t.Fatalf("expected StartElection to move to join, got %q", got.Status)
	}
}

func TestIntegration_CancelElection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustUser(t, s, "cancel-host")

	e, err := s.CreateElection(ctx, CreateElectionParams{ChannelID: "cancel-chan", CreatedBy: "cancel-host"})
	if err != nil {
		t.Fatalf("CreateElection: %v", err)
	}
	if err := s.CancelElection(ctx, e.ID); err != nil {
		t.Fatalf("CancelElection: %v", err)
	}
	active, err := s.GetActiveElection(ctx, "cancel-chan")
	if err != nil {
		t.Fatalf("GetActiveElection: %v", err)
	}
	if active != nil {
		t.Fatalf("expected no active election after cancellation, got %+v", active)
	}
}
