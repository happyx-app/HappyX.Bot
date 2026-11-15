package bot

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
	"github.com/happyx-app/HappyX.Bot/internal/schedule"
)

// The survey for whoever doesn't get HappyX's DMs: the first time a survey DM fails because the
// person has DMs from the server closed, HappyX posts one message in the team channel with
// «Answer today». The button opens that day's survey as a message only the person sees, so the
// answer stays as private as in a DM. The message is posted once and kept; it is posted again
// only when it is gone or the team channel changed. It never shows who answered or how many.

const actionAnswer = "answer" // button on the channel's survey message: open the day's survey privately

// closedDMs reports whether err is Discord refusing a DM because the user doesn't take DMs from
// the server's members (or blocked the bot).
func closedDMs(err error) bool {
	var re *discordgo.RESTError
	return errors.As(err, &re) && re.Message != nil && re.Message.Code == discordgo.ErrCodeCannotSendMessagesToThisUser
}

// surveyPost is the team channel's message for answering the survey without DMs.
func surveyPost(l *i18n.Locale, g api.Guild) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Author:      &discordgo.MessageEmbedAuthor{Name: l.T("survey.title", "guild", g.Name)},
			Description: l.T("post.text"),
			Color:       surveyColor,
		}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label: l.T("post.answer_button"), Emoji: &discordgo.ComponentEmoji{Name: "📝"}, Style: discordgo.PrimaryButton,
				CustomID: surveyID{Action: actionAnswer, GuildID: g.DiscordID}.String(),
			},
		}}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// ensureSurveyPost makes sure the team channel has the survey's message, after a survey DM failed
// because someone has DMs closed. Without a team channel there is nowhere to post it.
func (b *Bot) ensureSurveyPost(ctx context.Context, g api.Guild) {
	channel := g.TeamChannel()
	if channel == "" {
		b.log.Info("survey DMs closed and no team channel to post the survey in", "guild", g.DiscordID)
		return
	}
	p, err := b.api.ChannelPost(ctx, g.DiscordID, api.PostSurvey)
	switch {
	case errors.Is(err, api.ErrNotFound):
	case err != nil:
		b.log.Warn("read the survey's channel post", "guild", g.DiscordID, "err", err)
		return
	case p.ChannelID == channel:
		_, err := b.dg.ChannelMessage(p.ChannelID, p.MessageID, discordgo.WithContext(ctx))
		if err == nil || !gone(err) {
			return // still there, or Discord didn't say it's gone: don't post a second one
		}
	default:
		// The team channel changed: the message moves with it.
		if err := b.dg.ChannelMessageDelete(p.ChannelID, p.MessageID); err != nil && !gone(err) {
			b.log.Warn("delete the survey's old channel post", "guild", g.DiscordID, "err", err)
		}
	}
	sent, err := b.dg.ChannelMessageSendComplex(channel, surveyPost(i18n.Get(g.Language), g))
	if err != nil {
		b.log.Warn("post the survey in the team channel", "guild", g.DiscordID, "err", err)
		return
	}
	if err := b.api.PutChannelPost(ctx, g.DiscordID, api.PostSurvey, sent.ChannelID, sent.ID); err != nil {
		b.log.Warn("record the survey's channel post", "guild", g.DiscordID, "err", err)
	}
}

// openSurveyDay is the survey a member can answer now: today's once it has gone out, otherwise
// yesterday's if yesterday had one, as the DM keeps it open until today's arrives.
func (b *Bot) openSurveyDay(g api.Guild) (string, bool) {
	st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
	if day, due := schedule.SurveyDue(st, b.now()); due {
		return day, true
	}
	yesterday := guildToday(g, b.now()).AddDate(0, 0, -1)
	if st.Works(yesterday.Weekday()) {
		return yesterday.Format(isoDate), true
	}
	return "", false
}

// answerFromChannel is «Answer today» pressed in the team channel: the open survey, or the
// answer already given to it, as a message only the member sees.
func (b *Bot) answerFromChannel(ctx context.Context, i *discordgo.InteractionCreate, id surveyID) {
	g, l := b.guildAndLocale(ctx, id.GuildID)
	if i.GuildID != id.GuildID || g.DiscordID == "" {
		b.ephemeral(i, l.T("error.generic"))
		return
	}
	user := interactionUser(i)
	if !g.SendsSurvey() {
		b.ephemeral(i, l.T("post.off", "guild", g.Name))
		return
	}
	day, ok := b.openSurveyDay(g)
	if !ok {
		st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
		next, _ := schedule.NextSurvey(st, b.now())
		b.ephemeral(i, l.T("post.none", "when", fmt.Sprintf("<t:%d:F>", next.Unix())))
		return
	}
	// Reading the member's state takes a few calls: answer within Discord's 3 seconds first.
	if err := b.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		b.log.Error("interaction defer", "err", err)
		return
	}
	edit := func(data *discordgo.InteractionResponseData) {
		embeds, components := data.Embeds, data.Components
		content := data.Content
		if _, err := b.dg.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content, Embeds: &embeds, Components: &components, AllowedMentions: data.AllowedMentions,
		}); err != nil {
			b.log.Error("interaction edit", "err", err)
		}
	}
	// Who got that day's survey: before today's goes out, the open one is yesterday's, and a
	// vacation that started or ended today doesn't change who could answer it.
	recipients, err := b.api.SurveyRecipientsOn(ctx, g.DiscordID, day, false)
	if err != nil {
		edit(&discordgo.InteractionResponseData{Content: l.T("error.generic")})
		return
	}
	at := slices.IndexFunc(recipients, func(m api.Member) bool { return m.DiscordID == user.ID })
	if at < 0 {
		edit(&discordgo.InteractionResponseData{Content: l.T("post.not_yours", "guild", g.Name, "status_cmd", b.mention(cmdStatus))})
		return
	}
	moods, err := b.api.MemberMoods(ctx, g.DiscordID, user.ID, day, day)
	if err != nil {
		edit(&discordgo.InteractionResponseData{Content: l.T("error.generic")})
		return
	}
	// While guessing is open, the card carries the game as the DM's survey does: whoever answers
	// here gets no game DM either.
	var guess *guessInSurvey
	if q := b.openGuess(ctx, g); q != nil && q.plays(user.ID) {
		guess = q
	}
	if len(moods) > 0 {
		answered := surveyID{Action: actionMood, GuildID: g.DiscordID, Day: day, Mood: moods[0].Mood}
		data := b.thanks(ctx, l, g, answered, user, moods[0].Message)
		if guess != nil {
			data = guess.carried(l, g).onto(data)
		}
		edit(data)
		return
	}
	to := surveyRecipient{ID: user.ID, Name: b.userName(g.DiscordID, user), Yesterday: b.yesterday(g, day)}
	if recipients[at].PolicyPending {
		to.PolicyURL = b.policyURL()
	}
	msg := surveyMessage(l, g, day, to, b.guildIcon(g.DiscordID))
	if guess != nil {
		msg = withGame(l, g, msg, guess)
	}
	edit(&discordgo.InteractionResponseData{Embeds: msg.Embeds, Components: msg.Components, AllowedMentions: msg.AllowedMentions})
}

// mention is a clickable /happyx sub once the commands are registered, or its text.
func (b *Bot) mention(sub string) string {
	if id := b.commandID.Load(); id != nil {
		return fmt.Sprintf("</happyx %s:%s>", sub, *id)
	}
	return "`/happyx " + sub + "`"
}
