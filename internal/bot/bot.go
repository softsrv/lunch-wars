// Package bot wires MunchBot's slash commands to Discord via discordgo and
// drives each election through its join/nominate/veto/vote lifecycle.
package bot

import (
	"context"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/stockbauer/munchbot/internal/store"
)

// Bot owns the Discord session and database handle, and dispatches slash
// command interactions to their handlers.
type Bot struct {
	Session *discordgo.Session
	Store   *store.Store

	// GuildID, if set, scopes slash command registration to a single guild
	// for near-instant propagation during development. Empty means global
	// commands, which can take up to an hour to appear everywhere.
	GuildID string

	registeredCommandIDs []string
}

// New creates a Bot and opens its Discord session (without connecting yet).
func New(token string, st *store.Store, guildID string) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuilds

	b := &Bot{Session: session, Store: st, GuildID: guildID}
	session.AddHandler(b.onInteractionCreate)
	return b, nil
}

// Start opens the Discord gateway connection and registers slash commands.
func (b *Bot) Start() error {
	if err := b.Session.Open(); err != nil {
		return err
	}
	return b.registerCommands()
}

// Stop deregisters slash commands (best-effort) and closes the gateway
// connection.
func (b *Bot) Stop() {
	for _, id := range b.registeredCommandIDs {
		if err := b.Session.ApplicationCommandDelete(b.Session.State.User.ID, b.GuildID, id); err != nil {
			log.Printf("munchbot: failed to deregister command %s: %v", id, err)
		}
	}
	if err := b.Session.Close(); err != nil {
		log.Printf("munchbot: error closing session: %v", err)
	}
}

func (b *Bot) registerCommands() error {
	created, err := b.Session.ApplicationCommandBulkOverwrite(b.Session.State.User.ID, b.GuildID, commandDefinitions)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(created))
	for _, c := range created {
		ids = append(ids, c.ID)
	}
	b.registeredCommandIDs = ids
	return nil
}

func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	name := i.ApplicationCommandData().Name
	handler, ok := commandHandlers[name]
	if !ok {
		log.Printf("munchbot: no handler registered for command %q", name)
		return
	}

	if err := handler(ctx, b, s, i); err != nil {
		log.Printf("munchbot: handler for %q returned error: %v", name, err)
		// Best-effort fallback: if the handler failed before responding at
		// all, let the user know rather than leaving Discord's "thinking..."
		// spinner to time out.
		_ = respondEphemeral(s, i, "Something went wrong handling that command. Please try again.")
	}
}
