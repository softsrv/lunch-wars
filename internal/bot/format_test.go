package bot

import (
	"testing"
	"time"

	"github.com/stockbauer/munchbot/internal/store"
)

func TestSplitAndDedupe(t *testing.T) {
	got, err := splitAndDedupe(" Situ, cracklemi ,situ, MarketPho", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Situ", "cracklemi", "MarketPho"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSplitAndDedupe_TooMany(t *testing.T) {
	_, err := splitAndDedupe("a, b, c, d", 3)
	if err == nil {
		t.Fatal("expected error for exceeding max")
	}
}

func TestSplitAndDedupe_Empty(t *testing.T) {
	_, err := splitAndDedupe("  , , ", 3)
	if err == nil {
		t.Fatal("expected error for empty list")
	}
}

func TestResolveRestaurant(t *testing.T) {
	list := []store.Restaurant{
		{ID: 1, DisplayName: "Situ", NameNormalized: "situ"},
		{ID: 2, DisplayName: "MarketPho", NameNormalized: "marketpho"},
	}

	if r, ok := resolveRestaurant(list, "2"); !ok || r.ID != 2 {
		t.Fatalf("expected index lookup to find MarketPho, got %+v ok=%v", r, ok)
	}
	if r, ok := resolveRestaurant(list, " SITU "); !ok || r.ID != 1 {
		t.Fatalf("expected case-insensitive name match, got %+v ok=%v", r, ok)
	}
	if _, ok := resolveRestaurant(list, "0"); ok {
		t.Fatal("expected index 0 to be out of range")
	}
	if _, ok := resolveRestaurant(list, "3"); ok {
		t.Fatal("expected out-of-range index to fail")
	}
	if _, ok := resolveRestaurant(list, "nonexistent"); ok {
		t.Fatal("expected unknown name to fail")
	}
}

func TestParseScheduleTime_Empty(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseScheduleTime("", now)
	if err != nil || got != nil {
		t.Fatalf("expected nil, nil for empty input, got %v, %v", got, err)
	}
}

func TestParseScheduleTime_RelativeDuration(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseScheduleTime("in 30m", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(30 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseScheduleTime_ClockTimeLaterToday(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseScheduleTime("14:30", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseScheduleTime_ClockTimeAlreadyPassedRollsToTomorrow(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, err := parseScheduleTime("09:00", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseScheduleTime_Invalid(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := parseScheduleTime("whenever", now); err == nil {
		t.Fatal("expected error for unparseable time")
	}
}
