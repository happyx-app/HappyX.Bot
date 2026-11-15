package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// «Tell the team»: from the report, an admin writes what they saw and what they'll do, and the
// bot publishes it in the team channel. Right after, they can edit or delete it, and the next
// survey day's thanks card has a button to read it.

const teamPrefix = "hxt"

const (
	teamTell      = "tell"      // button on the report: open the modal (From, To)
	teamTellModal = "tellmodal" // modal submit: publish (From, To)
	teamEdit      = "edit"      // button after publishing: open the modal with the text (Note)
	teamEditModal = "editmodal" // modal submit: change the text (Note)
	teamDelete    = "del"       // button after publishing: ask to confirm (Note)
	teamDeleteOK  = "delok"     // button: delete it (Note)
	teamBack      = "back"      // button: back to what was published (Note)
)

// teamID is the custom ID of «Tell the team»'s components. The report is a DM, so the guild and
// the period travel in it; the user comes from the interaction, and is checked to manage the
// guild before anything is published.
type teamID struct {
	Action  string
	GuildID string
	From    string // the report's period, for teamTell and teamTellModal
	To      string
	Note    int64 // the published note, for the rest
}

func (t teamID) String() string {
	if t.Note != 0 {
		return fmt.Sprintf("%s:%s:%s:%d", teamPrefix, t.Action, t.GuildID, t.Note)
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s", teamPrefix, t.Action, t.GuildID, t.From, t.To)
}

func parseTeamID(v string) (teamID, bool) {
	p := strings.Split(v, ":")
	if len(p) < 4 || p[0] != teamPrefix || !snowflake(p[2]) {
		return teamID{}, false
	}
	id := teamID{Action: p[1], GuildID: p[2]}
	switch id.Action {
	case teamTell, teamTellModal:
		if len(p) != 5 || !isDate(p[3]) || !isDate(p[4]) {
			return teamID{}, false
		}
		id.From, id.To = p[3], p[4]
	case teamEdit, teamEditModal, teamDelete, teamDeleteOK, teamBack:
		n, err := strconv.ParseInt(p[3], 10, 64)
		if len(p) != 4 || err != nil || n <= 0 {
			return teamID{}, false
		}
		id.Note = n
	default:
		return teamID{}, false
	}
	return id, true
}

func isDate(s string) bool {
	_, err := time.Parse(isoDate, s)
	return err == nil
}

// handleTeam answers «Tell the team»'s buttons and modals.
func (b *Bot) handleTeam(ctx context.Context, i *discordgo.InteractionCreate, id teamID) {
	g, l := b.guildAndLocale(ctx, id.GuildID)
	user := interactionUser(i)
	if !b.managesGuild(ctx, i, l, g, user) {
		return
	}
	switch id.Action {
	case teamTell:
		// Told already about this period: show that note, to see, edit or delete it, rather than post
		// a second one.
		if n, err := b.api.PeriodTeamNote(ctx, id.GuildID, id.From, id.To); err == nil && b.noteAlive(ctx, id.GuildID, n) {
			if n.AuthorDiscordID != user.ID {
				// Another admin's note: theirs to change, as it goes out under their name.
				b.answer(i, published(l, id.GuildID, n, l.T("team.told_by", "user", "<@"+n.AuthorDiscordID+">", "channel", "<#"+n.ChannelID+">"), false))
				return
			}
			b.answer(i, published(l, id.GuildID, n, l.T("team.published", "channel", "<#"+n.ChannelID+">"), true))
			return
		} else if err != nil && !errors.Is(err, api.ErrNotFound) {
			b.replyError(i, l, err)
			return
		}
		channel := g.TeamChannel()
		if channel == "" {
			b.ephemeral(i, l.T("team.no_channel", "cmd", "/happyx "+l.T("cmd.config.name")+" "+l.T("opt.team_channel.name")))
			return
		}
		b.respond(i, discordgo.InteractionResponseModal, teamModal(l, teamID{Action: teamTellModal, GuildID: id.GuildID, From: id.From, To: id.To}, b.channelName(channel), ""))
	case teamTellModal:
		b.publishNote(ctx, i, l, g, user, id, modalValue(i.ModalSubmitData(), "body"))
	case teamEdit:
		n, ok := b.note(ctx, i, l, id, user)
		if !ok {
			return
		}
		b.respond(i, discordgo.InteractionResponseModal, teamModal(l, teamID{Action: teamEditModal, GuildID: id.GuildID, Note: n.ID}, b.channelName(n.ChannelID), n.Body))
	case teamEditModal:
		b.editNote(ctx, i, l, g, user, id, modalValue(i.ModalSubmitData(), "body"))
	case teamDelete:
		n, ok := b.note(ctx, i, l, id, user)
		if !ok {
			return
		}
		b.answer(i, &discordgo.InteractionResponseData{
			Content: l.T("team.delete_confirm", "channel", "<#"+n.ChannelID+">"),
			Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{Label: l.T("team.cancel"), Style: discordgo.SecondaryButton, CustomID: teamID{Action: teamBack, GuildID: id.GuildID, Note: n.ID}.String()},
				discordgo.Button{Label: l.T("team.delete_button"), Style: discordgo.DangerButton, CustomID: teamID{Action: teamDeleteOK, GuildID: id.GuildID, Note: n.ID}.String()},
			}}},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
	case teamDeleteOK:
		b.deleteNote(ctx, i, l, user, id)
	case teamBack:
		n, ok := b.note(ctx, i, l, id, user)
		if !ok {
			return
		}
		b.answer(i, published(l, id.GuildID, n, l.T("team.published", "channel", "<#"+n.ChannelID+">"), true))
	}
}

// answer replaces the private message the button is on, or, for the report's own buttons and
// the forms they open, answers with a new private message: the report stays as it is.
func (b *Bot) answer(i *discordgo.InteractionCreate, data *discordgo.InteractionResponseData) {
	if i.Message != nil && i.Message.Flags&discordgo.MessageFlagsEphemeral != 0 {
		b.respond(i, discordgo.InteractionResponseUpdateMessage, data)
		return
	}
	data.Flags |= discordgo.MessageFlagsEphemeral
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, data)
}

// noteAliveFor is how long a note's message seen alive isn't checked again; noteAliveWait how long
// Discord gets to answer.
const (
	noteAliveFor  = 10 * time.Minute
	noteAliveWait = 1500 * time.Millisecond
)

// noteAlive reports whether a note's message is still in its channel. One deleted by hand in
// Discord is forgotten, so nothing links to it any more. When Discord doesn't say (no permission
// to read the channel, an outage), the note is taken as alive.
//
// It runs while answering a survey, which must answer within Discord's 3 seconds, so Discord gets
// a short time to say, and a message seen alive isn't asked about again for a while.
func (b *Bot) noteAlive(ctx context.Context, guildID string, n api.TeamNote) bool {
	if seen, ok := b.aliveNotes.Load(n.ID); ok {
		if b.now().Sub(seen.(time.Time)) < noteAliveFor {
			return true
		}
		b.aliveNotes.Delete(n.ID)
	}
	cctx, cancel := context.WithTimeout(ctx, noteAliveWait)
	defer cancel()
	_, err := b.dg.ChannelMessage(n.ChannelID, n.MessageID, discordgo.WithContext(cctx))
	if err == nil {
		b.aliveNotes.Store(n.ID, b.now())
	}
	if err != nil && gone(err) {
		b.aliveNotes.Delete(n.ID)
		if derr := b.api.DeleteTeamNote(ctx, guildID, n.ID); derr != nil && !errors.Is(derr, api.ErrNotFound) {
			b.log.Warn("forget team note deleted in Discord", "guild", guildID, "err", derr)
		}
		return false
	}
	return true
}

// managesGuild checks the user can change the guild's settings, the same as /happyx config. The
// buttons live in a DM, where Discord says nothing about the server, so only HappyX's own access
// counts.
func (b *Bot) managesGuild(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, g api.Guild, user *discordgo.User) bool {
	if g.DiscordID == "" {
		b.ephemeral(i, l.T("error.generic"))
		return false
	}
	level, err := b.api.Access(ctx, g.DiscordID, user.ID, false)
	if err != nil {
		b.replyError(i, l, err)
		return false
	}
	if accessRank(level) < accessRank(api.AccessManage) {
		b.ephemeral(i, l.T("admin.only", "admin", "<@"+g.AdminDiscordID+">"))
		return false
	}
	return true
}

// note reads the note a button acts on, and answers when it is gone or isn't the user's: a note
// goes out under its author's name, so only they change or delete it.
func (b *Bot) note(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, id teamID, user *discordgo.User) (api.TeamNote, bool) {
	n, err := b.api.TeamNote(ctx, id.GuildID, id.Note)
	if err == nil && n.AuthorDiscordID != user.ID {
		b.answer(i, &discordgo.InteractionResponseData{Content: l.T("team.not_yours", "user", "<@"+n.AuthorDiscordID+">"),
			Components: []discordgo.MessageComponent{}, AllowedMentions: &discordgo.MessageAllowedMentions{}})
		return n, false
	}
	if errors.Is(err, api.ErrNotFound) {
		b.answer(i, &discordgo.InteractionResponseData{Content: l.T("team.gone"), Components: []discordgo.MessageComponent{}})
		return n, false
	}
	if err != nil {
		b.replyError(i, l, err)
		return n, false
	}
	return n, true
}

// channelName is a channel's name as the bot knows it, for a modal's title, where mentions
// don't show. Empty if unknown.
func (b *Bot) channelName(channelID string) string {
	if b.dg == nil || b.dg.State == nil {
		return ""
	}
	if ch, err := b.dg.State.Channel(channelID); err == nil {
		return ch.Name
	}
	return ""
}

// teamModal asks what the admin wants to tell the team. Discord's modal has no button of its
// own (it always reads «Submit»), so the title says where it goes.
func teamModal(l *i18n.Locale, id teamID, channel, body string) *discordgo.InteractionResponseData {
	key := "team.modal_title"
	if id.Action == teamEditModal {
		key = "team.edit_title"
	}
	title := l.T(key + "_nochannel")
	if channel != "" {
		title = l.T(key, "channel", "#"+channel)
	}
	if r := []rune(title); len(r) > 45 { // Discord's limit
		title = string(r[:44]) + "…"
	}
	return &discordgo.InteractionResponseData{
		CustomID: id.String(),
		Title:    title,
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{
			CustomID:    "body",
			Label:       l.T("team.modal_label"),
			Style:       discordgo.TextInputParagraph,
			Required:    true,
			MinLength:   1,
			MaxLength:   api.TeamNoteMaxLength,
			Placeholder: l.T("team.modal_placeholder"),
			Value:       body,
		}}}},
	}
}

// teamCard is what the team sees in the channel: who tells them, about which period, and what.
func teamCard(l *i18n.Locale, n api.TeamNote) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{teamEmbed(l, n)},
		AllowedMentions: &discordgo.MessageAllowedMentions{}, // the author's mention shows without pinging
	}
}

func teamEmbed(l *i18n.Locale, n api.TeamNote) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Description: "### 💬 " + l.T("team.card_title", "user", "<@"+n.AuthorDiscordID+">", "from", shortDate(l, n.From), "to", shortDate(l, n.To)) +
			"\n" + n.Body,
		Color: surveyColor,
	}
}

// published is the private answer once the note is in the channel: where, a link to it, and,
// for its author, buttons to edit or delete it.
func published(l *i18n.Locale, guildID string, n api.TeamNote, text string, own bool) *discordgo.InteractionResponseData {
	row := []discordgo.MessageComponent{
		discordgo.Button{Label: l.T("team.see_button"), Style: discordgo.LinkButton, URL: messageLink(guildID, n.ChannelID, n.MessageID)},
	}
	if own {
		row = append(row,
			discordgo.Button{Label: l.T("team.edit_button"), Style: discordgo.SecondaryButton, CustomID: teamID{Action: teamEdit, GuildID: guildID, Note: n.ID}.String()},
			discordgo.Button{Label: l.T("team.delete_button"), Style: discordgo.SecondaryButton, CustomID: teamID{Action: teamDelete, GuildID: guildID, Note: n.ID}.String()},
		)
	}
	return &discordgo.InteractionResponseData{
		Content:         text,
		Components:      []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}},
		Flags:           discordgo.MessageFlagsEphemeral,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// publishNote posts the note in the team channel and records it. If the channel refuses it, the
// admin gets their text back rather than lose it.
func (b *Bot) publishNote(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, g api.Guild, user *discordgo.User, id teamID, body string) {
	body = strings.TrimSpace(body)
	channel := g.TeamChannel()
	if channel == "" {
		b.ephemeral(i, l.T("team.no_channel", "cmd", "/happyx "+l.T("cmd.config.name")+" "+l.T("opt.team_channel.name"))+"\n"+quote(body))
		return
	}
	// Usually another admin told the team while this one was writing: show theirs, post nothing.
	if first, err := b.api.PeriodTeamNote(ctx, g.DiscordID, id.From, id.To); err == nil && b.noteAlive(ctx, g.DiscordID, first) {
		b.answer(i, toldAlready(l, g.DiscordID, first, user, body))
		return
	}
	n := api.TeamNote{AuthorDiscordID: user.ID, From: id.From, To: id.To, Body: body, ChannelID: channel}
	m, err := b.dg.ChannelMessageSendComplex(channel, teamCard(l, n))
	if err != nil {
		b.log.Warn("team note post failed", "guild", g.DiscordID, "channel", channel, "err", err)
		var text string
		switch {
		case gone(err):
			text = l.T("team.channel_gone", "cmd", "/happyx "+l.T("cmd.config.name")+" "+l.T("opt.team_channel.name"))
		case forbidden(err):
			text = l.T("team.cant_post", "channel", "<#"+channel+">")
		default:
			text = l.T("team.post_failed")
		}
		b.ephemeral(i, text+"\n"+quote(body))
		return
	}
	n.MessageID = m.ID
	saved, err := b.api.PublishTeamNote(ctx, g.DiscordID, n)
	if errors.Is(err, api.ErrConflict) {
		// Another admin told the team about this period a moment ago: one note per period.
		key := "team.told_first"
		if derr := b.dg.ChannelMessageDelete(channel, m.ID); derr != nil {
			b.log.Warn("take back second team note", "guild", g.DiscordID, "err", derr)
			key = "team.told_first_kept"
		}
		if first, ferr := b.api.PeriodTeamNote(ctx, g.DiscordID, id.From, id.To); ferr == nil {
			b.answer(i, published(l, g.DiscordID, first,
				l.T(key, "user", "<@"+first.AuthorDiscordID+">", "channel", "<#"+first.ChannelID+">")+"\n"+quote(body), first.AuthorDiscordID == user.ID))
			return
		}
	}
	if err != nil {
		// Unrecorded, it could be neither edited nor deleted from here: take it back.
		if derr := b.dg.ChannelMessageDelete(channel, m.ID); derr != nil {
			b.log.Warn("take back unrecorded team note", "guild", g.DiscordID, "err", derr)
		}
		b.log.Error("record team note", "guild", g.DiscordID, "err", err)
		b.ephemeral(i, l.T("error.generic")+"\n"+quote(body))
		return
	}
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, published(l, g.DiscordID, saved, l.T("team.published", "channel", "<#"+channel+">"), true))
}

// editNote changes the message in the channel, then the note.
func (b *Bot) editNote(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, g api.Guild, user *discordgo.User, id teamID, body string) {
	n, ok := b.note(ctx, i, l, id, user)
	if !ok {
		return
	}
	n.Body = strings.TrimSpace(body)
	embeds := []*discordgo.MessageEmbed{teamEmbed(l, n)}
	if _, err := b.dg.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: n.ChannelID, ID: n.MessageID, Embeds: &embeds}); err != nil {
		if gone(err) {
			_ = b.api.DeleteTeamNote(ctx, g.DiscordID, n.ID)
			b.answer(i, &discordgo.InteractionResponseData{Content: l.T("team.gone") + "\n" + quote(n.Body), Components: []discordgo.MessageComponent{}})
			return
		}
		b.log.Warn("edit team note", "guild", g.DiscordID, "err", err)
		b.ephemeral(i, l.T("team.cant_edit", "channel", "<#"+n.ChannelID+">")+"\n"+quote(n.Body))
		return
	}
	saved, err := b.api.EditTeamNote(ctx, g.DiscordID, n.ID, n.Body)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	b.answer(i, published(l, g.DiscordID, saved, l.T("team.edited", "channel", "<#"+n.ChannelID+">"), true))
}

// deleteNote takes the message out of the channel, then forgets the note.
func (b *Bot) deleteNote(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User, id teamID) {
	n, ok := b.note(ctx, i, l, id, user)
	if !ok {
		return
	}
	if err := b.dg.ChannelMessageDelete(n.ChannelID, n.MessageID); err != nil && !gone(err) {
		b.log.Warn("delete team note", "guild", id.GuildID, "err", err)
		b.ephemeral(i, l.T("team.cant_delete", "channel", "<#"+n.ChannelID+">"))
		return
	}
	if err := b.api.DeleteTeamNote(ctx, id.GuildID, n.ID); err != nil && !errors.Is(err, api.ErrNotFound) {
		b.replyError(i, l, err)
		return
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
		Content: l.T("team.deleted", "channel", "<#"+n.ChannelID+">"), Components: []discordgo.MessageComponent{},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
}

// toldAlready answers someone who wrote a note for a period that already has one: the note there
// is, with their own text below so they don't lose it.
func toldAlready(l *i18n.Locale, guildID string, n api.TeamNote, user *discordgo.User, body string) *discordgo.InteractionResponseData {
	if n.AuthorDiscordID == user.ID {
		return published(l, guildID, n, l.T("team.told_self", "channel", "<#"+n.ChannelID+">")+"\n"+quote(body), true)
	}
	return published(l, guildID, n, l.T("team.told_by", "user", "<@"+n.AuthorDiscordID+">", "channel", "<#"+n.ChannelID+">")+" "+l.T("team.wrote")+"\n"+quote(body), false)
}

// gone reports whether Discord says the message or its channel no longer exists.
func gone(err error) bool {
	var re *discordgo.RESTError
	return errors.As(err, &re) && re.Response != nil && re.Response.StatusCode == http.StatusNotFound
}

// forbidden reports whether Discord refused for lack of permissions.
func forbidden(err error) bool {
	var re *discordgo.RESTError
	return errors.As(err, &re) && re.Response != nil && re.Response.StatusCode == http.StatusForbidden
}

// toldButton is the thanks card's link to what an admin told the team the survey day before, or
// nil if nobody did.
func (b *Bot) toldButton(ctx context.Context, l *i18n.Locale, guildID, day string) *discordgo.Button {
	n, err := b.api.CurrentTeamNote(ctx, guildID, day)
	if err != nil {
		if !errors.Is(err, api.ErrNotFound) {
			b.log.Warn("current team note", "guild", guildID, "err", err)
		}
		return nil
	}
	if !b.noteAlive(ctx, guildID, n) {
		return nil
	}
	name := b.displayName(guildID, api.Member{DiscordID: n.AuthorDiscordID, Username: n.AuthorUsername})
	return &discordgo.Button{
		Label: truncate(l.T("answer.told_button", "name", name), 80),
		Style: discordgo.LinkButton, URL: messageLink(guildID, n.ChannelID, n.MessageID),
	}
}

// withTold adds to a thanks card the link to what an admin told the team, on the survey day
// after they did.
func (b *Bot) withTold(ctx context.Context, l *i18n.Locale, id surveyID, data *discordgo.InteractionResponseData) *discordgo.InteractionResponseData {
	btn := b.toldButton(ctx, l, id.GuildID, id.Day)
	if btn == nil || len(data.Components) == 0 {
		return data
	}
	if row, ok := data.Components[0].(discordgo.ActionsRow); ok {
		row.Components = append(row.Components, *btn)
		data.Components[0] = row
	}
	return data
}

// reportOptions completes a report: the period before to compare with, for a completed period,
// the dashboard's link, and «Tell the team» for the report's own DM.
func (b *Bot) reportOptions(ctx context.Context, g api.Guild, r api.Report, completed bool) reportOptions {
	o := reportOptions{Monthly: g.ReportFrequency == "monthly", Tell: completed}
	preset := "this-week"
	if o.Monthly {
		preset = "this-month"
	}
	if completed {
		preset = strings.Replace(preset, "this", "last", 1)
		if from, to, ok := previousPeriod(r.From, o.Monthly); ok {
			if p, err := b.api.Report(ctx, g.DiscordID, from, to); err == nil {
				o.Previous = &p
			} else {
				b.log.Warn("previous report", "guild", g.DiscordID, "err", err)
			}
		}
	}
	if b.webURL != "" {
		o.DashboardURL = b.webURL + "/#/g/" + g.DiscordID + "?p=" + preset
	}
	return o
}

// previousPeriod is the week or month before the one starting on from.
func previousPeriod(from string, monthly bool) (string, string, bool) {
	start, err := time.Parse(isoDate, from)
	if err != nil {
		return "", "", false
	}
	if monthly {
		prev := start.AddDate(0, -1, 0)
		return prev.Format(isoDate), start.AddDate(0, 0, -1).Format(isoDate), true
	}
	return start.AddDate(0, 0, -7).Format(isoDate), start.AddDate(0, 0, -1).Format(isoDate), true
}

// summaryMinPeople is the fewest people who must have answered before the summary says how many
// did: with fewer, the team can tell who didn't. The server's own minimum applies when higher.
const summaryMinPeople = 5

// teamSummary is the past week in the team channel: the game's results, the kudos and how many
// answered. Never the team's mood. Nil when there is nothing to say.
func teamSummary(l *i18n.Locale, g api.Guild, week string, r api.Report, round *api.GameRound, gameURL string) *discordgo.MessageSend {
	var lines []string
	var buttons []discordgo.MessageComponent
	if round != nil && round.Phase == "finished" && round.Answers >= max(round.MinAnswers, 1) {
		lines = append(lines, l.T("summary.game", "question", round.Question))
		if gameURL != "" {
			buttons = append(buttons, discordgo.Button{Label: l.T("summary.game_button"), Emoji: &discordgo.ComponentEmoji{Name: "🎲"}, Style: discordgo.LinkButton, URL: gameURL})
		}
	}
	if k := r.Kudos; k.Count > 0 {
		if k.Receivers == 1 {
			lines = append(lines, l.T("summary.kudos_one", "n", k.Count))
		} else {
			lines = append(lines, l.T("summary.kudos", "n", k.Count, "people", k.Receivers))
		}
	}
	if g.SendsSurvey() && r.Respondents >= max(summaryMinPeople, g.Threshold()) {
		if r.Audience >= r.Respondents {
			lines = append(lines, l.T("summary.answered_of", "n", r.Respondents, "of", r.Audience))
		} else {
			lines = append(lines, l.T("summary.answered", "n", r.Respondents))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	monday, _ := time.Parse(isoDate, week)
	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Description: "### 📅 " + l.T("summary.title", "guild", g.Name, "from", shortDate(l, week), "to", shortDate(l, monday.AddDate(0, 0, 6).Format(isoDate))) +
				"\n" + strings.Join(lines, "\n"),
			Color: surveyColor,
		}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
	if len(buttons) > 0 {
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
	}
	return msg
}

// sendTeamSummary posts the past week's summary in the team channel, if there is anything to say.
func (b *Bot) sendTeamSummary(ctx context.Context, g api.Guild, week string) (delivery, error) {
	monday, err := time.Parse(isoDate, week)
	if err != nil {
		return delivery{}, err
	}
	r, err := b.api.Report(ctx, g.DiscordID, week, monday.AddDate(0, 0, 6).Format(isoDate))
	if err != nil {
		return delivery{}, err
	}
	var round *api.GameRound
	if g.GamesEnabled {
		rd, err := b.api.GameRound(ctx, g.DiscordID, week)
		switch {
		case err == nil:
			round = &rd
		case !errors.Is(err, api.ErrNotFound):
			return delivery{}, err
		}
	}
	gameURL := ""
	if b.webURL != "" {
		gameURL = b.webURL + "/#/game/" + g.DiscordID + "?history"
	}
	msg := teamSummary(i18n.Get(g.Language), g, week, r, round, gameURL)
	if msg == nil {
		b.log.Info("team summary skipped: nothing to say", "guild", g.DiscordID, "week", week)
		return delivery{}, nil
	}
	if _, err := b.dg.ChannelMessageSendComplex(g.TeamChannel(), msg); err != nil {
		return delivery{recipients: 1, failed: 1}, err
	}
	return delivery{recipients: 1}, nil
}

// teamChannelLine says where HappyX tells the team things and whether the Monday summary goes
// there, for /happyx status and config.
func teamChannelLine(l *i18n.Locale, g api.Guild) string {
	var line string
	switch {
	case g.TeamChannelID != "":
		line = l.T("team.channel", "channel", "<#"+g.TeamChannelID+">")
	case g.KudosChannelID != "":
		line = l.T("team.channel_kudos", "channel", "<#"+g.KudosChannelID+">")
	default:
		return l.T("team.channel_none")
	}
	if g.TeamSummary {
		line += " · " + l.T("team.summary_on")
	}
	return line
}

// closedDMsWarning tells admins that people with DMs closed didn't get the last survey and, with
// no team channel, have nowhere else to answer it.
func (b *Bot) closedDMsWarning(l *i18n.Locale, g api.Guild, closed int) string {
	if closed == 0 || g.TeamChannel() != "" {
		return ""
	}
	key := "team.closed_dms"
	if closed == 1 {
		key = "team.closed_dms_one"
	}
	return "\n" + l.T(key, "count", closed, "config_cmd", b.mention(cmdConfig))
}

// teamChannelWarning says HappyX can't post in the team channel, where the Monday summary and the
// notes would fail; empty when it can or there is none.
func (b *Bot) teamChannelWarning(l *i18n.Locale, g api.Guild) string {
	ch := g.TeamChannel()
	if ch == "" || b.dg == nil || b.canPost(ch) {
		return ""
	}
	return "\n" + l.T("team.channel_cant", "channel", "<#"+ch+">")
}
