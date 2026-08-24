package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stockbauer/munchbot/internal/rcv"
	"github.com/stockbauer/munchbot/internal/store"
)

// splitAndDedupe splits a comma-separated list, trims whitespace, drops
// empty entries, and removes case-insensitive duplicates (keeping the first
// occurrence). It returns an error if more than max distinct items remain.
func splitAndDedupe(raw string, max int) ([]string, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no restaurants given")
	}
	if len(out) > max {
		return nil, fmt.Errorf("you listed %d restaurants, but %d is the max", len(out), max)
	}
	return out, nil
}

// resolveRestaurant matches user input against a candidate list, either by
// its 1-based position in the list or by a case-insensitive exact name
// match.
func resolveRestaurant(list []store.Restaurant, raw string) (*store.Restaurant, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n >= 1 && n <= len(list) {
			r := list[n-1]
			return &r, true
		}
		return nil, false
	}
	normalized := strings.ToLower(raw)
	for _, r := range list {
		if r.NameNormalized == normalized {
			rr := r
			return &rr, true
		}
	}
	return nil, false
}

// formatRestaurantList renders a numbered list of restaurants, 1-indexed.
func formatRestaurantList(list []store.Restaurant) string {
	if len(list) == 0 {
		return "_(no restaurants yet)_"
	}
	var b strings.Builder
	for idx, r := range list {
		fmt.Fprintf(&b, "%d. %s\n", idx+1, r.DisplayName)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatParticipants renders a mention list of joined participants.
func formatParticipants(userIDs []string) string {
	if len(userIDs) == 0 {
		return "_(nobody yet)_"
	}
	mentions := make([]string, len(userIDs))
	for i, id := range userIDs {
		mentions[i] = fmt.Sprintf("<@%s>", id)
	}
	return strings.Join(mentions, ", ")
}

// formatRCVResult renders the full round-by-round instant-runoff tally plus
// the final winner announcement.
func formatRCVResult(result rcv.Result) string {
	var b strings.Builder
	b.WriteString("🍽️ **Ranked-choice results**\n\n")
	for i, round := range result.Rounds {
		fmt.Fprintf(&b, "**Round %d**\n", i+1)
		for _, t := range round.Tallies {
			fmt.Fprintf(&b, "- %s — %d vote(s)\n", t.Name, t.Votes)
		}
		if round.Eliminated != nil {
			fmt.Fprintf(&b, "❌ Eliminated: %s\n", round.Eliminated.Name)
		}
		if round.Winner != nil {
			fmt.Fprintf(&b, "🏆 %s wins this round!\n", round.Winner.Name)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## 🎉 Winner: %s! 🎉", result.Winner.Name)
	return b.String()
}

// parseScheduleTime interprets the optional /lunch time argument. It
// accepts a relative duration ("in 30m", "in 1h15m") or an absolute 24-hour
// clock time ("12:30"), which resolves to the next occurrence of that time
// from now. An empty string means "start immediately" (nil, nil).
func parseScheduleTime(raw string, now time.Time) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if rest, ok := strings.CutPrefix(strings.ToLower(raw), "in "); ok {
		d, err := time.ParseDuration(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("couldn't parse %q as a duration (try 'in 30m' or 'in 1h15m')", raw)
		}
		if d <= 0 {
			return nil, fmt.Errorf("duration must be positive")
		}
		t := now.Add(d)
		return &t, nil
	}

	clock, err := time.ParseInLocation("15:04", raw, now.Location())
	if err != nil {
		return nil, fmt.Errorf("couldn't parse %q as a time (try '12:30' or 'in 30m')", raw)
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return &t, nil
}
