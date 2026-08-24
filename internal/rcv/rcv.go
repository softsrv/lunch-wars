// Package rcv implements instant-runoff ranked-choice voting.
package rcv

import "sort"

// Ballot is one voter's ranked choices, in preference order (index 0 = 1st choice).
// CandidateIDs must reference keys in the Candidates map passed to Run.
type Ballot struct {
	VoterID      string
	CandidateIDs []int64
}

// Candidate is an eligible option in the election.
type Candidate struct {
	ID   int64
	Name string
}

// RoundTally is the vote count for one candidate within one round.
type RoundTally struct {
	CandidateID int64
	Name        string
	Votes       int
}

// Round captures the full tally and elimination decision for one round of counting.
type Round struct {
	Tallies    []RoundTally // sorted descending by votes, then name
	Eliminated *RoundTally  // nil if this round produced a winner instead
	Winner     *RoundTally  // nil unless this round produced a majority (or final) winner
}

// Result is the full outcome of running instant-runoff voting to completion.
type Result struct {
	Rounds []Round
	Winner Candidate
}

// Run executes instant-runoff voting over the given candidates and ballots.
// It eliminates the lowest vote-getter each round (ties broken by candidate
// name, ascending) until one candidate holds a strict majority of the
// non-exhausted ballots, or only one candidate remains.
//
// Run panics if candidates is empty; callers must enforce the minimum of 3
// remaining candidates before invoking a real vote.
func Run(candidates []Candidate, ballots []Ballot) Result {
	if len(candidates) == 0 {
		panic("rcv: Run called with no candidates")
	}

	names := make(map[int64]string, len(candidates))
	for _, c := range candidates {
		names[c.ID] = c.Name
	}

	remaining := make(map[int64]bool, len(candidates))
	for _, c := range candidates {
		remaining[c.ID] = true
	}

	var rounds []Round

	for {
		counts := make(map[int64]int, len(remaining))
		for id := range remaining {
			counts[id] = 0
		}

		activeBallots := 0
		for _, b := range ballots {
			for _, cid := range b.CandidateIDs {
				if remaining[cid] {
					counts[cid]++
					activeBallots++
					break
				}
			}
		}

		tallies := make([]RoundTally, 0, len(counts))
		for id, v := range counts {
			tallies = append(tallies, RoundTally{CandidateID: id, Name: names[id], Votes: v})
		}
		sort.Slice(tallies, func(i, j int) bool {
			if tallies[i].Votes != tallies[j].Votes {
				return tallies[i].Votes > tallies[j].Votes
			}
			return tallies[i].Name < tallies[j].Name
		})

		// Majority winner (or only one candidate left, or nobody voted at all).
		if len(tallies) == 1 || (activeBallots > 0 && tallies[0].Votes*2 > activeBallots) {
			winner := tallies[0]
			rounds = append(rounds, Round{Tallies: tallies, Winner: &winner})
			return Result{Rounds: rounds, Winner: Candidate{ID: winner.CandidateID, Name: winner.Name}}
		}

		// Eliminate the lowest vote-getter (last in the sorted, tie-broken slice).
		loser := tallies[len(tallies)-1]
		round := Round{Tallies: tallies, Eliminated: &loser}
		rounds = append(rounds, round)
		delete(remaining, loser.CandidateID)
	}
}
