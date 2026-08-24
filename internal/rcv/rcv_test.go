package rcv

import "testing"

func TestRun_MajorityFirstRound(t *testing.T) {
	candidates := []Candidate{{1, "A"}, {2, "B"}, {3, "C"}}
	ballots := []Ballot{
		{"u1", []int64{1}},
		{"u2", []int64{1}},
		{"u3", []int64{1}},
		{"u4", []int64{2}},
		{"u5", []int64{3}},
	}
	res := Run(candidates, ballots)
	if res.Winner.ID != 1 {
		t.Fatalf("expected candidate 1 to win, got %d", res.Winner.ID)
	}
	if len(res.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d", len(res.Rounds))
	}
}

func TestRun_EliminationTransfersVotes(t *testing.T) {
	candidates := []Candidate{{1, "A"}, {2, "B"}, {3, "C"}}
	// C has fewest first-place votes and is eliminated; its ballot's second
	// choice (B) should push B over the majority threshold.
	ballots := []Ballot{
		{"u1", []int64{1}},
		{"u2", []int64{1}},
		{"u3", []int64{2}},
		{"u4", []int64{2}},
		{"u5", []int64{3, 2}},
	}
	res := Run(candidates, ballots)
	if res.Winner.ID != 2 {
		t.Fatalf("expected candidate 2 to win, got %d", res.Winner.ID)
	}
	if len(res.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(res.Rounds))
	}
	if res.Rounds[0].Eliminated == nil || res.Rounds[0].Eliminated.CandidateID != 3 {
		t.Fatalf("expected candidate 3 eliminated in round 1")
	}
}

func TestRun_ExhaustedBallotsDontCountTowardMajority(t *testing.T) {
	candidates := []Candidate{{1, "A"}, {2, "B"}, {3, "C"}}
	// u3's ballot only ranks C; once C is eliminated the ballot is exhausted
	// and drops out of the active-ballot count entirely.
	ballots := []Ballot{
		{"u1", []int64{1}},
		{"u2", []int64{2}},
		{"u3", []int64{3}},
	}
	res := Run(candidates, ballots)
	if res.Winner.ID != 1 && res.Winner.ID != 2 {
		t.Fatalf("expected A or B (tie-break by name) to win, got %d", res.Winner.ID)
	}
	// Tie-break is alphabetical by name: "A" < "B", so A should win.
	if res.Winner.ID != 1 {
		t.Fatalf("expected candidate 1 (A) to win tie-break, got %d", res.Winner.ID)
	}
}

func TestRun_SingleCandidate(t *testing.T) {
	candidates := []Candidate{{1, "OnlyOne"}}
	ballots := []Ballot{{"u1", []int64{1}}}
	res := Run(candidates, ballots)
	if res.Winner.ID != 1 {
		t.Fatalf("expected sole candidate to win")
	}
	if len(res.Rounds) != 1 {
		t.Fatalf("expected 1 round, got %d", len(res.Rounds))
	}
}

func TestRun_NoVotesAtAll(t *testing.T) {
	candidates := []Candidate{{1, "A"}, {2, "B"}, {3, "C"}}
	res := Run(candidates, nil)
	// With no ballots, every round has zero counted votes; the algorithm
	// should still terminate by eliminating down to one candidate.
	if res.Winner.ID != 1 {
		t.Fatalf("expected candidate 1 (A) to win by alphabetical tie-break, got %d", res.Winner.ID)
	}
}
