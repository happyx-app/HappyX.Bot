package bot

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// The survey's «Options» and /happyx status give each member the same few things for one
// server's survey: a vacation, a pause (confirmed first) and a link to their own evolution on the
// web. They answer with ephemeral messages, so the survey card itself never grows.

// optionsButtons are the member's actions for g's survey. msg is the survey message a pause
// should replace, empty when the options don't come from a survey.
func (b *Bot) optionsButtons(l *i18n.Locale, g api.Guild, day, msg string, paused bool) []discordgo.MessageComponent {
	row := []discordgo.MessageComponent{
		discordgo.Button{
			Label: l.T("options.vacation_button"), Emoji: &discordgo.ComponentEmoji{Name: "🌴"}, Style: discordgo.SecondaryButton,
			CustomID: surveyID{Action: actionVacation, GuildID: g.DiscordID, Day: day}.String(),
		},
	}
	if paused {
		row = append(row, discordgo.Button{
			Label: l.T("survey.resume_button"), Emoji: &discordgo.ComponentEmoji{Name: "▶️"}, Style: discordgo.SecondaryButton,
			CustomID: surveyID{Action: actionResume, GuildID: g.DiscordID, Day: day}.String(),
		})
	} else {
		row = append(row, discordgo.Button{
			Label: l.T("options.pause_button"), Emoji: &discordgo.ComponentEmoji{Name: "⏸️"}, Style: discordgo.SecondaryButton,
			CustomID: surveyID{Action: actionPause, GuildID: g.DiscordID, Day: day, Msg: msg}.String(),
		})
	}
	if link := b.evolutionURL(g.DiscordID); link != "" {
		row = append(row, discordgo.Button{
			Label: l.T("options.evolution_button"), Emoji: &discordgo.ComponentEmoji{Name: "📈"}, Style: discordgo.LinkButton, URL: link,
		})
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}}
}

// evolutionURL is the member's own page on the web for one server, or empty without WEB_URL.
func (b *Bot) evolutionURL(guildID string) string {
	if b.webURL == "" {
		return ""
	}
	return b.webURL + "/#/me?g=" + guildID
}

// surveySchedule is when g's survey goes out, like «De lunes a viernes a las 13:00 (Europe/Madrid).».
func surveySchedule(l *i18n.Locale, g api.Guild) string {
	return l.T("options.schedule", "days", capitalized(daysLabel(l, g.WorkingDays)), "hour", fmt.Sprintf("%02d", g.SurveyHour), "timezone", g.Timezone)
}

// capitalized is s with its first letter in upper case, to start a sentence.
func capitalized(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// privacyLine says what the admins see of the member's answers on day.
func privacyLine(l *i18n.Locale, g api.Guild, day string) string {
	switch {
	case g.NamedSoon(day):
		return l.T("options.anonymous_today", "date", shortDate(l, g.NamedSince))
	case g.AnonymousOn(day):
		return l.T("options.anonymous", "k", g.Threshold())
	}
	return l.T("options.named")
}

func (b *Bot) optionsMessage(l *i18n.Locale, g api.Guild, day, msg string) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		Content:         l.T("options.text", "guild", g.Name, "schedule", surveySchedule(l, g)) + "\n" + privacyLine(l, g, guildToday(g, b.now()).Format(isoDate)),
		Components:      b.optionsButtons(l, g, day, msg, false),
		Flags:           discordgo.MessageFlagsEphemeral,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// handleOptions runs the survey's options buttons. In a DM the guild comes from the custom ID;
// the API checks the member belongs to it.
func (b *Bot) handleOptions(ctx context.Context, i *discordgo.InteractionCreate, id surveyID, g api.Guild, l *i18n.Locale) {
	user := interactionUser(i)
	switch id.Action {
	case actionOptions:
		msg := i.Message.ID
		if i.Message.Flags&discordgo.MessageFlagsEphemeral != 0 {
			msg = "" // a survey answered from the channel: only the one in the DM can be paused
		}
		b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, b.optionsMessage(l, g, id.Day, msg))
	case actionCancel:
		data := b.optionsMessage(l, g, id.Day, id.Msg)
		data.Flags = 0 // updating the ephemeral message keeps it ephemeral
		b.respond(i, discordgo.InteractionResponseUpdateMessage, data)
	case actionPause:
		b.respond(i, discordgo.InteractionResponseUpdateMessage, pauseConfirm(l, g, id))
	case actionPauseOK:
		if err := b.api.RemoveMember(ctx, id.GuildID, user.ID); err != nil && !errors.Is(err, api.ErrNotFound) {
			b.replyError(i, l, err)
			return
		}
		content := l.T("options.paused_cmd", "guild", g.Name, "resume_cmd", b.resumeMention())
		switch {
		case id.Msg != "" && surveyOpen(g, id.Day, b.now()):
			// The survey card turns into the paused one, which can bring the survey back. An
			// older card, closed since the options were opened, stays as it is.
			if _, err := b.dg.ChannelMessageEditComplex(pausedEdit(l, g, id.Day, i.ChannelID, id.Msg)); err != nil {
				b.log.Warn("show paused survey", "guild", id.GuildID, "err", err)
			} else {
				content = l.T("options.paused", "guild", g.Name)
			}
		case id.Msg == "":
			// From /happyx status or a survey answered in the channel: the open survey in the
			// DM, if there is one, turns into the paused card all the same.
			if b.syncSurveyCard(l, g, user, true) {
				content = l.T("options.paused", "guild", g.Name)
			}
		}
		b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
			Content: content, Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
	case actionResume:
		ended, err := b.resume(ctx, g, user)
		if err != nil {
			b.replyError(i, l, err)
			return
		}
		if i.Message != nil && i.Message.Flags&discordgo.MessageFlagsEphemeral == 0 {
			// The paused survey card: today's survey comes back to be answered.
			b.respond(i, discordgo.InteractionResponseUpdateMessage, b.resumedSurvey(l, g, id.Day, user))
			if ended != nil {
				b.followUp(i, vacationEndedText(l, g.Name, *ended))
			}
			return
		}
		b.syncSurveyCard(l, g, user, false)
		b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
			Content: resumedText(l, g.Name, ended), Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
	case actionVacation:
		b.respond(i, discordgo.InteractionResponseModal, vacationModal(l, id))
	}
}

// pauseConfirm asks before pausing, with the pause last, as Discord's own dialogs do.
func pauseConfirm(l *i18n.Locale, g api.Guild, id surveyID) *discordgo.InteractionResponseData {
	confirm, cancel := id, id
	confirm.Action, cancel.Action = actionPauseOK, actionCancel
	return &discordgo.InteractionResponseData{
		Content: l.T("options.pause_confirm", "guild", g.Name),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: l.T("options.cancel"), Style: discordgo.SecondaryButton, CustomID: cancel.String()},
			discordgo.Button{Label: l.T("options.pause_button"), Emoji: &discordgo.ComponentEmoji{Name: "⏸️"}, Style: discordgo.DangerButton, CustomID: confirm.String()},
		}}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// pausedEdit turns a survey card into the paused one, with a button to get the survey again.
func pausedEdit(l *i18n.Locale, g api.Guild, day, channelID, messageID string) *discordgo.MessageEdit {
	embeds := []*discordgo.MessageEmbed{{
		Author:      &discordgo.MessageEmbedAuthor{Name: l.T("survey.title", "guild", g.Name)},
		Description: l.T("survey.paused"),
		Color:       closedColor,
	}}
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			Label: l.T("survey.resume_button"), Emoji: &discordgo.ComponentEmoji{Name: "▶️"}, Style: discordgo.SecondaryButton,
			CustomID: surveyID{Action: actionResume, GuildID: g.DiscordID, Day: day}.String(),
		},
	}}}
	empty := ""
	return &discordgo.MessageEdit{Channel: channelID, ID: messageID, Content: &empty, Embeds: &embeds, Components: &components}
}

// resumedSurvey is what the paused card turns into: the survey again while it is still open
// (today's or yesterday's), or, for an older day, a grey card saying the next one will come.
func (b *Bot) resumedSurvey(l *i18n.Locale, g api.Guild, day string, user *discordgo.User) *discordgo.InteractionResponseData {
	if !surveyOpen(g, day, b.now()) {
		return &discordgo.InteractionResponseData{
			Content: "",
			Embeds: []*discordgo.MessageEmbed{{
				Author:      &discordgo.MessageEmbedAuthor{Name: l.T("survey.title", "guild", g.Name)},
				Description: l.T("survey.resumed"),
				Color:       closedColor,
			}},
			Components:      []discordgo.MessageComponent{},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}
	}
	msg := surveyMessage(l, g, day, surveyRecipient{ID: user.ID, Name: b.userName(g.DiscordID, user), Yesterday: b.yesterday(g, day)}, b.guildIcon(g.DiscordID))
	return &discordgo.InteractionResponseData{Embeds: msg.Embeds, Components: msg.Components, AllowedMentions: msg.AllowedMentions}
}

// yesterday reports whether day is before today in g's timezone: a survey still open after
// midnight.
func (b *Bot) yesterday(g api.Guild, day string) bool {
	return day < guildToday(g, b.now()).Format(isoDate)
}

// surveyOpen reports whether day's survey can still be answered: today's and yesterday's, as
// the API allows.
func surveyOpen(g api.Guild, day string, now time.Time) bool {
	today := guildToday(g, now)
	return day >= today.AddDate(0, 0, -1).Format(isoDate) && day <= today.Format(isoDate)
}

func vacationModal(l *i18n.Locale, id surveyID) *discordgo.InteractionResponseData {
	id.Action = actionVacModal
	input := func(field, label, placeholder string) discordgo.MessageComponent {
		return discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
			CustomID: field, Label: label, Style: discordgo.TextInputShort, Required: true, MaxLength: 10, Placeholder: placeholder,
		}}}
	}
	return &discordgo.InteractionResponseData{
		CustomID: id.String(),
		Title:    l.T("vacation.modal_title"),
		Components: []discordgo.MessageComponent{
			input(optFrom, l.T("vacation.modal_from"), "20/12"),
			input(optTo, l.T("vacation.modal_to"), "6/1"),
		},
	}
}

// handleVacationModal sets the vacation typed in the modal. On success the options turn into the
// confirmation; a date it can't read gets its own reply, so the options stay to try again.
func (b *Bot) handleVacationModal(ctx context.Context, i *discordgo.InteractionCreate, id surveyID, data discordgo.ModalSubmitInteractionData) {
	g, l := b.guildAndLocale(ctx, id.GuildID)
	text, ok, err := b.setVacation(ctx, g, l, interactionUser(i), modalValue(data, optFrom), modalValue(data, optTo))
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	if !ok {
		b.ephemeral(i, text)
		return
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
		Content: text, Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
}
