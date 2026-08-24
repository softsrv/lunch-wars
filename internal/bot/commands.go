package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/stockbauer/munchbot/internal/rcv"
	"github.com/stockbauer/munchbot/internal/store"
)

var commandDefinitions = []*discordgo.ApplicationCommand{
	{
		Name:        "lunch",
		Description: "Start a lunch decision vote",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "time",
				Description: "When to start, e.g. '12:30' or 'in 30m' (default: now)",
				Required:    false,
			},
		},
	},
	{
		Name:        "join",
		Description: "Join the current lunch vote",
	},
	{
		Name:        "nominate",
		Description: "Nominate up to 3 restaurants, comma-separated",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "restaurants",
				Description: "e.g. 'situ, cracklemi, marketpho'",
				Required:    true,
			},
		},
	},
	{
		Name:        "veto",
		Description: "Veto one restaurant from the list",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "restaurant",
				Description: "Name or list number to veto",
				Required:    true,
			},
		},
	},
	{
		Name:        "vote",
		Description: "Rank up to 3 restaurants in order of preference, comma-separated",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "restaurants",
				Description: "e.g. 'marketpho, situ, cracklemi'",
				Required:    true,
			},
		},
	},
	{
		Name:        "next",
		Description: "(Host only) Close the current phase and advance to the next",
	},
	{
		Name:        "cancel",
		Description: "(Host only) Cancel the current lunch vote",
	},
}

type commandHandler func(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error

var commandHandlers = map[string]commandHandler{
	"lunch":    handleLunch,
	"join":     handleJoin,
	"nominate": handleNominate,
	"veto":     handleVeto,
	"vote":     handleVote,
	"next":     handleNext,
	"cancel":   handleCancel,
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) error {
	data := &discordgo.InteractionResponseData{Content: content}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	})
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	return respond(s, i, content, true)
}

func respondPublic(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	return respond(s, i, content, false)
}

// interactionUser returns the invoking user, whether the interaction came
// from a guild channel (Member set) or a DM (User set directly).
func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

func optionString(i *discordgo.InteractionCreate, name string) string {
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}

// requireActiveElection fetches the channel's active election, responding
// with an ephemeral error and returning ok=false if there isn't one.
func requireActiveElection(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) (store.Election, bool) {
	e, err := b.Store.GetActiveElection(ctx, i.ChannelID)
	if err != nil {
		log.Printf("munchbot: GetActiveElection: %v", err)
		_ = respondEphemeral(s, i, "Something went wrong looking up the current vote.")
		return store.Election{}, false
	}
	if e == nil {
		_ = respondEphemeral(s, i, "There's no lunch vote in progress here. Start one with /lunch.")
		return store.Election{}, false
	}
	return *e, true
}

// joinAnnouncement is posted whenever an election opens for joining,
// whether started immediately or by the scheduler.
const joinAnnouncement = "🍽️ **Time to decide on lunch!** Use `/join` to participate. Once everyone's in, the host runs `/next` to start nominations."

func handleLunch(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	user := interactionUser(i)
	if err := b.Store.UpsertUser(ctx, user.ID, user.Username); err != nil {
		return err
	}

	scheduledAt, err := parseScheduleTime(optionString(i, "time"), time.Now())
	if err != nil {
		return respondEphemeral(s, i, "⚠️ "+err.Error())
	}

	var guildID *string
	if i.GuildID != "" {
		guildID = &i.GuildID
	}

	_, err = b.Store.CreateElection(ctx, store.CreateElectionParams{
		GuildID:     guildID,
		ChannelID:   i.ChannelID,
		CreatedBy:   user.ID,
		ScheduledAt: scheduledAt,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return respondEphemeral(s, i, "There's already a lunch vote in progress here. Finish it or /cancel it first.")
		}
		return err
	}

	if scheduledAt != nil {
		return respondEphemeral(s, i, fmt.Sprintf("🍽️ Lunch vote scheduled for %s.", scheduledAt.Local().Format("Mon Jan 2, 3:04 PM")))
	}

	return respondPublic(s, i, joinAnnouncement)
}

func handleJoin(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	if e.Status != store.StatusJoin {
		return respondEphemeral(s, i, "Joining isn't open right now.")
	}

	user := interactionUser(i)
	if err := b.Store.UpsertUser(ctx, user.ID, user.Username); err != nil {
		return err
	}
	if err := b.Store.AddParticipant(ctx, e.ID, user.ID); err != nil {
		return err
	}
	n, err := b.Store.CountParticipants(ctx, e.ID)
	if err != nil {
		return err
	}
	return respondPublic(s, i, fmt.Sprintf("✅ <@%s> joined the lunch vote! (%d joined so far)", user.ID, n))
}

func handleNominate(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	if e.Status != store.StatusNominate {
		return respondEphemeral(s, i, "Nominations aren't open right now.")
	}

	user := interactionUser(i)
	if err := b.Store.UpsertUser(ctx, user.ID, user.Username); err != nil {
		return err
	}

	isParticipant, err := b.Store.IsParticipant(ctx, e.ID, user.ID)
	if err != nil {
		return err
	}
	if !isParticipant {
		return respondEphemeral(s, i, "You need to /join the vote before nominating.")
	}

	names, err := splitAndDedupe(optionString(i, "restaurants"), store.MaxNominationsPerUser)
	if err != nil {
		return respondEphemeral(s, i, "⚠️ "+err.Error())
	}

	already, err := b.Store.CountNominationsByUser(ctx, e.ID, user.ID)
	if err != nil {
		return err
	}
	if already+len(names) > store.MaxNominationsPerUser {
		return respondEphemeral(s, i, fmt.Sprintf("You've already nominated %d restaurant(s); you can nominate %d total.", already, store.MaxNominationsPerUser))
	}

	var added, dup []string
	for _, name := range names {
		_, err := b.Store.AddNomination(ctx, e.ID, user.ID, name)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				dup = append(dup, name)
				continue
			}
			return err
		}
		added = append(added, name)
	}

	var b2 strings.Builder
	if len(added) > 0 {
		fmt.Fprintf(&b2, "🍴 Nominated: %s.", strings.Join(added, ", "))
	}
	if len(dup) > 0 {
		if b2.Len() > 0 {
			b2.WriteString(" ")
		}
		fmt.Fprintf(&b2, "(Already nominated: %s.)", strings.Join(dup, ", "))
	}
	return respondEphemeral(s, i, b2.String())
}

func handleVeto(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	if e.Status != store.StatusVeto {
		return respondEphemeral(s, i, "Vetoes aren't open right now.")
	}

	user := interactionUser(i)
	if err := b.Store.UpsertUser(ctx, user.ID, user.Username); err != nil {
		return err
	}

	isParticipant, err := b.Store.IsParticipant(ctx, e.ID, user.ID)
	if err != nil {
		return err
	}
	if !isParticipant {
		return respondEphemeral(s, i, "You need to have joined the vote to veto.")
	}

	hasVetoed, err := b.Store.HasVetoed(ctx, e.ID, user.ID)
	if err != nil {
		return err
	}
	if hasVetoed {
		return respondEphemeral(s, i, "You've already used your veto for this vote.")
	}

	// Resolve against the *full* nomination list (stable, original
	// numbering) rather than the shrinking active-only list — otherwise a
	// restaurant's list number would drift every time someone else vetoes,
	// and a number a user saw in the announcement could silently point at
	// the wrong restaurant by the time they use it.
	all, err := b.Store.ListRestaurants(ctx, e.ID, false)
	if err != nil {
		return err
	}
	r, found := resolveRestaurant(all, optionString(i, "restaurant"))
	if !found {
		return respondEphemeral(s, i, "Couldn't find that restaurant on the current list. Use its name or list number.")
	}
	if r.Vetoed {
		return respondEphemeral(s, i, fmt.Sprintf("**%s** has already been vetoed.", r.DisplayName))
	}

	active, err := b.Store.CountActiveRestaurants(ctx, e.ID)
	if err != nil {
		return err
	}
	if active-1 < store.MinFinalChoices {
		return respondEphemeral(s, i, fmt.Sprintf("Can't veto — only %d restaurant(s) remain and at least %d must remain for the final vote.", active, store.MinFinalChoices))
	}

	if err := b.Store.AddVeto(ctx, e.ID, user.ID, r.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return respondEphemeral(s, i, "You've already used your veto for this vote.")
		}
		return err
	}

	remaining, err := b.Store.CountActiveRestaurants(ctx, e.ID)
	if err != nil {
		return err
	}
	return respondPublic(s, i, fmt.Sprintf("🚫 <@%s> vetoed **%s**. %d restaurant(s) remain.", user.ID, r.DisplayName, remaining))
}

func handleVote(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	if e.Status != store.StatusVote {
		return respondEphemeral(s, i, "Voting isn't open right now.")
	}

	user := interactionUser(i)
	if err := b.Store.UpsertUser(ctx, user.ID, user.Username); err != nil {
		return err
	}

	isParticipant, err := b.Store.IsParticipant(ctx, e.ID, user.ID)
	if err != nil {
		return err
	}
	if !isParticipant {
		return respondEphemeral(s, i, "You need to have joined the vote to vote.")
	}

	names, err := splitAndDedupe(optionString(i, "restaurants"), store.MaxVotesPerUser)
	if err != nil {
		return respondEphemeral(s, i, "⚠️ "+err.Error())
	}

	active, err := b.Store.ListRestaurants(ctx, e.ID, true)
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(names))
	var unresolved []string
	var picked []string
	for _, name := range names {
		r, found := resolveRestaurant(active, name)
		if !found {
			unresolved = append(unresolved, name)
			continue
		}
		ids = append(ids, r.ID)
		picked = append(picked, r.DisplayName)
	}
	if len(unresolved) > 0 {
		return respondEphemeral(s, i, fmt.Sprintf("⚠️ Couldn't match: %s. Use exact names from the list or their list numbers.", strings.Join(unresolved, ", ")))
	}

	if err := b.Store.ReplaceVotes(ctx, e.ID, user.ID, ids); err != nil {
		return err
	}

	var rb strings.Builder
	rb.WriteString("🗳️ Your ranked vote: ")
	for idx, name := range picked {
		fmt.Fprintf(&rb, "%d) %s ", idx+1, name)
	}
	return respondEphemeral(s, i, strings.TrimSpace(rb.String()))
}

func handleCancel(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	user := interactionUser(i)
	if user.ID != e.CreatedBy {
		return respondEphemeral(s, i, "Only the person who started this lunch vote can cancel it.")
	}
	if err := b.Store.CancelElection(ctx, e.ID); err != nil {
		return err
	}
	return respondPublic(s, i, "🛑 Lunch vote cancelled.")
}

func handleNext(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	e, ok := requireActiveElection(ctx, b, s, i)
	if !ok {
		return nil
	}
	user := interactionUser(i)
	if user.ID != e.CreatedBy {
		return respondEphemeral(s, i, "Only the person who started this lunch vote can advance it.")
	}

	switch e.Status {
	case store.StatusPending:
		return respondEphemeral(s, i, "This lunch vote hasn't started yet.")

	case store.StatusJoin:
		participants, err := b.Store.ListParticipantIDs(ctx, e.ID)
		if err != nil {
			return err
		}
		if len(participants) == 0 {
			return respondEphemeral(s, i, "Nobody has /join'd yet.")
		}
		if err := b.Store.SetElectionStatus(ctx, e.ID, store.StatusNominate); err != nil {
			return err
		}
		return respondPublic(s, i, fmt.Sprintf(
			"🍴 **Nominations are open!** %s — submit up to 3 restaurants with `/nominate`.",
			formatParticipants(participants),
		))

	case store.StatusNominate:
		restaurants, err := b.Store.ListRestaurants(ctx, e.ID, false)
		if err != nil {
			return err
		}
		if len(restaurants) < store.MinFinalChoices {
			return respondEphemeral(s, i, fmt.Sprintf("Need at least %d nominated restaurants before continuing (currently %d). Ask people to /nominate more!", store.MinFinalChoices, len(restaurants)))
		}
		if err := b.Store.SetElectionStatus(ctx, e.ID, store.StatusVeto); err != nil {
			return err
		}
		return respondPublic(s, i, fmt.Sprintf(
			"📋 **Nominations are in!**\n%s\n\n🚫 Each of you may `/veto` one restaurant to knock it off the list.",
			formatRestaurantList(restaurants),
		))

	case store.StatusVeto:
		remaining, err := b.Store.ListRestaurants(ctx, e.ID, true)
		if err != nil {
			return err
		}
		if len(remaining) < store.MinFinalChoices {
			return respondEphemeral(s, i, fmt.Sprintf("Only %d restaurant(s) remain; need at least %d to vote.", len(remaining), store.MinFinalChoices))
		}
		if err := b.Store.SetElectionStatus(ctx, e.ID, store.StatusVote); err != nil {
			return err
		}
		return respondPublic(s, i, fmt.Sprintf(
			"🗳️ **Vetoes are in! Time for the final vote.**\n%s\n\nRank up to 3 choices in order of preference with `/vote`, e.g. `/vote %s`.",
			formatRestaurantList(remaining), remaining[0].DisplayName,
		))

	case store.StatusVote:
		return finishVoting(ctx, b, s, i, e)

	default:
		return respondEphemeral(s, i, "This lunch vote has already finished.")
	}
}

func finishVoting(ctx context.Context, b *Bot, s *discordgo.Session, i *discordgo.InteractionCreate, e store.Election) error {
	voterCount, err := b.Store.CountVoters(ctx, e.ID)
	if err != nil {
		return err
	}
	if voterCount == 0 {
		return respondEphemeral(s, i, "Nobody has voted yet.")
	}

	restaurants, err := b.Store.ListRestaurants(ctx, e.ID, true)
	if err != nil {
		return err
	}
	votes, err := b.Store.ListVotes(ctx, e.ID)
	if err != nil {
		return err
	}

	candidates := make([]rcv.Candidate, len(restaurants))
	for idx, r := range restaurants {
		candidates[idx] = rcv.Candidate{ID: r.ID, Name: r.DisplayName}
	}

	ballotsByVoter := make(map[string][]int64)
	for _, v := range votes {
		ballotsByVoter[v.UserID] = append(ballotsByVoter[v.UserID], v.RestaurantID)
	}
	ballots := make([]rcv.Ballot, 0, len(ballotsByVoter))
	for voter, ids := range ballotsByVoter {
		ballots = append(ballots, rcv.Ballot{VoterID: voter, CandidateIDs: ids})
	}

	result := rcv.Run(candidates, ballots)
	summary := formatRCVResult(result)

	if err := b.Store.CompleteElection(ctx, e.ID, result.Winner.ID, summary); err != nil {
		return err
	}
	return respondPublic(s, i, summary)
}
