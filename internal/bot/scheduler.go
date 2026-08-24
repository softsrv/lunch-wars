package bot

import (
	"context"
	"log"
	"time"

	"github.com/stockbauer/munchbot/internal/store"
)

// pollInterval is how often the scheduler checks for elections whose
// scheduled start time has arrived. Lunch scheduling doesn't need
// second-level precision, so a modest interval keeps DB load negligible.
const pollInterval = 20 * time.Second

// RunScheduler polls for due scheduled elections and starts them, blocking
// until ctx is cancelled. Run it in its own goroutine.
func (b *Bot) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.startDueElections(ctx)
		}
	}
}

func (b *Bot) startDueElections(ctx context.Context) {
	due, err := b.Store.ListDuePendingElections(ctx, time.Now().UTC())
	if err != nil {
		log.Printf("munchbot: listing due elections: %v", err)
		return
	}
	for _, e := range due {
		if err := b.startDueElection(ctx, e); err != nil {
			log.Printf("munchbot: starting scheduled election %d: %v", e.ID, err)
		}
	}
}

func (b *Bot) startDueElection(ctx context.Context, e store.Election) error {
	if err := b.Store.StartElection(ctx, e.ID); err != nil {
		return err
	}
	_, err := b.Session.ChannelMessageSend(e.ChannelID, joinAnnouncement)
	return err
}
