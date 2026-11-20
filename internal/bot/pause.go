package bot

import (
	"context"
	"errors"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// Pausing and resuming change the survey card in the member's DM wherever they are done from:
// the card's own options, /happyx status, /happyx pause or resume, in a server or in the DM.

// resume makes the member get the guild's survey again. Being back also ends the vacations going
// on today, and says so; the days already gone stay vacation, and one planned for later stays.
// It returns the vacations it ended. An API from before vacations answers 404.
func (b *Bot) resume(ctx context.Context, g api.Guild, user *discordgo.User) ([]api.Vacation, error) {
	yes := true
	if err := b.api.UpsertMember(ctx, g.DiscordID, user.ID, user.Username, b.userName(g.DiscordID, user), &yes); err != nil {
		return nil, err
	}
	ended, err := b.api.EndVacations(ctx, g.DiscordID, user.ID)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil
	}
	return ended, err
}

// resumedText is the reply to resuming g's survey, with the vacations that ended, if any.
func resumedText(l *i18n.Locale, guildName string, ended []api.Vacation) string {
	text := l.T("resume.done", "guild", guildName)
	if guildName == "" {
		text = l.T("resume.done_here")
	}
	for _, v := range ended {
		text += "\n" + vacationEndedText(l, guildName, v)
	}
	return text
}

// vacationEndedText says a vacation ended because the member came back: their own, in every
// server, or the one the admins of guildName set.
func vacationEndedText(l *i18n.Locale, guildName string, v api.Vacation) string {
	if v.SetByAdmin {
		return l.T("resume.vacation_ended_admin", "guild", guildName, "from", discordDate(v.From), "to", discordDate(v.To))
	}
	return l.T("resume.vacation_ended", "from", discordDate(v.From), "to", discordDate(v.To))
}

// syncSurveyCard turns g's open survey in the member's DM into the paused card, or the paused
// card back into the survey, after a pause or resume done elsewhere. It reports whether it
// changed a card. A survey already answered, or one that can no longer be answered, stays.
func (b *Bot) syncSurveyCard(l *i18n.Locale, g api.Guild, user *discordgo.User, paused bool) bool {
	ch, err := b.dg.UserChannelCreate(user.ID)
	if err != nil {
		b.log.Warn("open DM to update the survey", "guild", g.DiscordID, "err", err)
		return false
	}
	msgs, err := b.dg.ChannelMessages(ch.ID, oldSurveysChecked, "", "", "")
	if err != nil {
		b.log.Warn("read DM to update the survey", "guild", g.DiscordID, "err", err)
		return false
	}
	for _, m := range msgs { // newest first
		day, state, ok := surveyCard(m, g.DiscordID)
		if !ok || !surveyOpen(g, day, b.now()) {
			continue
		}
		var edit *discordgo.MessageEdit
		switch {
		case paused && state == cardOpen:
			edit = pausedEdit(l, g, day, m.ChannelID, m.ID)
		case !paused && state == cardPaused:
			data := b.resumedSurvey(l, g, day, user)
			edit = &discordgo.MessageEdit{Channel: m.ChannelID, ID: m.ID, Embeds: &data.Embeds, Components: &data.Components, AllowedMentions: data.AllowedMentions}
		default:
			return false // the latest survey card is answered, or already as it should be
		}
		if _, err := b.dg.ChannelMessageEditComplex(edit); err != nil {
			b.log.Warn("update the survey card", "guild", g.DiscordID, "err", err)
			return false
		}
		return true
	}
	return false
}

// The states of a survey card in a DM.
const (
	cardOpen     = iota // the faces, unanswered
	cardAnswered        // the answer, or the faces of «Change answer»
	cardPaused          // the paused card with «Get it again»
)

// surveyCard reads a DM for a survey card of guildID: its day and state.
func surveyCard(m *discordgo.Message, guildID string) (day string, state int, ok bool) {
	for _, id := range surveyIDs(m) {
		if id.GuildID != guildID {
			return "", 0, false
		}
		switch id.Action {
		case actionMood:
			return id.Day, cardOpen, true
		case actionResume:
			return id.Day, cardPaused, true
		case actionNote, actionChange, actionRemood:
			return id.Day, cardAnswered, true
		}
	}
	return "", 0, false
}

// followUp sends an ephemeral message after the interaction was answered.
func (b *Bot) followUp(i *discordgo.InteractionCreate, content string) {
	if _, err := b.dg.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: content, Flags: discordgo.MessageFlagsEphemeral, AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		b.log.Warn("interaction follow-up", "err", err)
	}
}
