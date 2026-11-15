package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
	"github.com/happyx-app/HappyX.Bot/internal/schedule"
)

// Command and option names are the English ones; Discord shows the localized names from the
// catalogs but always sends these back.
const (
	cmdStatus   = "status"
	cmdPause    = "pause"
	cmdResume   = "resume"
	cmdReport   = "report"
	cmdConfig   = "config"
	cmdKudos    = "kudos"
	cmdSetup    = "setup"
	cmdVacation = "vacation"

	// menuKudos is the right-click "Give kudos" on a user or a message; en.json's menu.kudos.
	menuKudos = "Give kudos"

	optAnonymous = "anonymous"
	optHour      = "hour"
	optTimezone  = "timezone"
	optFrequency = "frequency"
	optAdmin     = "admin"
	optLanguage  = "language"
	optPeriod    = "period"
	optUser      = "user"
	optMessage   = "message"
	optKudosChan = "kudos_channel"
	optGames     = "games"
	optWorkDays  = "working_days"
	optSurvey    = "survey"
	optFrom      = "from"
	optTo        = "to"

	periodLast    = "last"
	periodCurrent = "current"
)

func commands() []*discordgo.ApplicationCommand {
	no := false
	minHour := 0.0
	one := 1
	en := i18n.Get(i18n.Default)

	sub := func(name string, opts ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:                     discordgo.ApplicationCommandOptionSubCommand,
			Name:                     name,
			NameLocalizations:        *localized("cmd." + name + ".name"),
			Description:              en.T("cmd." + name + ".description"),
			DescriptionLocalizations: *localized("cmd." + name + ".description"),
			Options:                  opts,
		}
	}
	opt := func(t discordgo.ApplicationCommandOptionType, name string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:                     t,
			Name:                     name,
			NameLocalizations:        *localized("opt." + name + ".name"),
			Description:              en.T("opt." + name + ".description"),
			DescriptionLocalizations: *localized("opt." + name + ".description"),
		}
	}

	hour := opt(discordgo.ApplicationCommandOptionInteger, optHour)
	hour.MinValue, hour.MaxValue = &minHour, 23
	frequency := opt(discordgo.ApplicationCommandOptionString, optFrequency)
	for _, f := range []string{"weekly", "monthly"} {
		frequency.Choices = append(frequency.Choices, &discordgo.ApplicationCommandOptionChoice{
			Name: en.T("freq." + f), NameLocalizations: *localized("freq." + f), Value: f,
		})
	}
	period := opt(discordgo.ApplicationCommandOptionString, optPeriod)
	for _, p := range []string{periodLast, periodCurrent} {
		period.Choices = append(period.Choices, &discordgo.ApplicationCommandOptionChoice{
			Name: en.T("period." + p), NameLocalizations: *localized("period." + p), Value: p,
		})
	}
	kudosUser := opt(discordgo.ApplicationCommandOptionUser, optUser)
	kudosUser.Required = true
	kudosMessage := opt(discordgo.ApplicationCommandOptionString, optMessage)
	kudosMessage.Required, kudosMessage.MinLength, kudosMessage.MaxLength = true, &one, kudosMaxLength
	kudosChannel := opt(discordgo.ApplicationCommandOptionChannel, optKudosChan)
	kudosChannel.ChannelTypes = []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews}
	workingDays := opt(discordgo.ApplicationCommandOptionString, optWorkDays)
	for _, p := range workingDayPresets {
		workingDays.Choices = append(workingDays.Choices, &discordgo.ApplicationCommandOptionChoice{
			Name: en.T("days." + p.key), NameLocalizations: *localized("days." + p.key), Value: p.key,
		})
	}
	vacationFrom := opt(discordgo.ApplicationCommandOptionString, optFrom)
	vacationTo := opt(discordgo.ApplicationCommandOptionString, optTo)
	vacationFrom.Required, vacationTo.Required = true, true
	language := opt(discordgo.ApplicationCommandOptionString, optLanguage)
	for _, code := range i18n.Codes() {
		language.Choices = append(language.Choices, &discordgo.ApplicationCommandOptionChoice{
			Name: i18n.Get(code).Name, Value: code,
		})
	}

	kudosMenu := func(t discordgo.ApplicationCommandType) *discordgo.ApplicationCommand {
		return &discordgo.ApplicationCommand{Type: t, Name: menuKudos, NameLocalizations: localized("menu.kudos"), DMPermission: &no}
	}

	return []*discordgo.ApplicationCommand{{
		Name:                     "happyx",
		Description:              en.T("cmd.description"),
		DescriptionLocalizations: localized("cmd.description"),
		DMPermission:             &no,
		Options: []*discordgo.ApplicationCommandOption{
			sub(cmdStatus),
			sub(cmdPause),
			sub(cmdResume),
			sub(cmdVacation, vacationFrom, vacationTo),
			sub(cmdReport, period),
			sub(cmdKudos, kudosUser, kudosMessage),
			sub(cmdSetup),
			sub(cmdConfig,
				opt(discordgo.ApplicationCommandOptionBoolean, optSurvey),
				opt(discordgo.ApplicationCommandOptionBoolean, optAnonymous),
				hour,
				opt(discordgo.ApplicationCommandOptionString, optTimezone),
				frequency,
				language,
				opt(discordgo.ApplicationCommandOptionUser, optAdmin),
				kudosChannel,
				opt(discordgo.ApplicationCommandOptionBoolean, optGames),
				workingDays,
			),
		},
	}, kudosMenu(discordgo.UserApplicationCommand), kudosMenu(discordgo.MessageApplicationCommand)}
}

// localized builds Discord's per-locale map for key from every catalog that claims a Discord
// locale and differs from English.
func localized(key string) *map[discordgo.Locale]string {
	en := i18n.Get(i18n.Default).T(key)
	m := map[discordgo.Locale]string{}
	for _, code := range i18n.Codes() {
		l := i18n.Get(code)
		if !l.Has(key) || l.T(key) == en {
			continue
		}
		for _, d := range l.Discord {
			m[discordgo.Locale(d)] = l.T(key)
		}
	}
	return &m
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(ctx, i)
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		if id, ok := parseSetupID(customID); ok {
			b.handleSetup(ctx, i, id)
			return
		}
		if b.handleTestButton(ctx, i, customID) {
			return
		}
		if id, ok := parseGameID(i.MessageComponentData().CustomID); ok && (id.Action == gameAnswer || id.Action == gamePropose) {
			b.handleGameButton(ctx, i, id)
			return
		}
		b.handleSurveyButton(ctx, i)
	case discordgo.InteractionModalSubmit:
		data := i.ModalSubmitData()
		if id, ok := parseGameID(data.CustomID); ok && id.Action == gameAnswerModal {
			b.handleGameModal(ctx, i, id, data)
			return
		}
		if id, ok := parseGameID(data.CustomID); ok && id.Action == gameProposeModal {
			b.handleProposeModal(ctx, i, id, data)
			return
		}
		if id, ok := parseKudosID(data.CustomID); ok {
			b.handleKudosModal(ctx, i, id, data)
			return
		}
		b.handleNoteModal(ctx, i)
	}
}

func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

// guildLocale is the language configured for the guild, English if it can't be read.
func (b *Bot) guildLocale(ctx context.Context, guildID string) *i18n.Locale {
	_, l := b.guildAndLocale(ctx, guildID)
	return l
}

// guildAndLocale is guildLocale with the guild itself, which is empty if it can't be read.
func (b *Bot) guildAndLocale(ctx context.Context, guildID string) (api.Guild, *i18n.Locale) {
	g, err := b.api.Guild(ctx, guildID)
	if err != nil {
		return api.Guild{}, i18n.Get(i18n.Default)
	}
	return g, i18n.Get(g.Language)
}

// userName is how the server shows u, for a button pressed in a DM where Discord doesn't say.
func (b *Bot) userName(guildID string, u *discordgo.User) string {
	name := u.GlobalName
	if name == "" {
		name = u.Username
	}
	return b.displayName(guildID, api.Member{DiscordID: u.ID, Username: name})
}

// userLocale answers slash commands in the user's Discord language when the bot has it, and
// in the guild's language otherwise.
func (b *Bot) userLocale(ctx context.Context, i *discordgo.InteractionCreate) *i18n.Locale {
	if l, ok := i18n.FromDiscord(string(i.Locale)); ok {
		return l
	}
	return b.guildLocale(ctx, i.GuildID)
}

// --- Survey answers (in DMs, in the guild's language like the survey itself) ---

func (b *Bot) handleSurveyButton(ctx context.Context, i *discordgo.InteractionCreate) {
	id, ok := parseSurveyID(i.MessageComponentData().CustomID)
	if !ok {
		return
	}
	g, l := b.guildAndLocale(ctx, id.GuildID)
	user := interactionUser(i)
	switch id.Action {
	case actionMood:
		// Changing the mood keeps a comment given earlier that day.
		e, err := b.api.ChangeMood(ctx, id.GuildID, user.ID, id.Day, id.Mood)
		if err != nil {
			b.surveyError(i, l, g, id, err)
			return
		}
		b.respond(i, discordgo.InteractionResponseUpdateMessage, answeredMessage(l, g, id, b.userName(id.GuildID, user), e.Message))
	case actionChange:
		if !surveyOpen(g, id.Day, b.now()) {
			b.respond(i, discordgo.InteractionResponseUpdateMessage, closedSurvey(l, g, id.Day))
			return
		}
		msg := surveyMessage(l, g, id.Day, surveyRecipient{ID: user.ID, Name: b.userName(id.GuildID, user)}, b.guildIcon(id.GuildID))
		b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
			Content: "", Embeds: msg.Embeds, Components: msg.Components, AllowedMentions: msg.AllowedMentions,
		})
	case actionNote:
		b.respond(i, discordgo.InteractionResponseModal, noteModal(l, g, id))
	case actionStop: // surveys sent before «Options» still have this button
		err := b.api.RemoveMember(ctx, id.GuildID, user.ID)
		if err != nil && !errors.Is(err, api.ErrNotFound) {
			b.replyError(i, l, err)
			return
		}
		b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
			Content:    l.T("stop.done", "resume_cmd", b.resumeMention()),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		})
	default:
		b.handleOptions(ctx, i, id, g, l)
	}
}

// surveyError answers a failed survey answer. A survey too old to answer turns into the
// closed card, which is what it should have been already.
func (b *Bot) surveyError(i *discordgo.InteractionCreate, l *i18n.Locale, g api.Guild, id surveyID, err error) {
	if api.TooOld(err) {
		b.respond(i, discordgo.InteractionResponseUpdateMessage, closedSurvey(l, g, id.Day))
		return
	}
	b.replyError(i, l, err)
}

func (b *Bot) handleNoteModal(ctx context.Context, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	id, ok := parseSurveyID(data.CustomID)
	if !ok {
		return
	}
	if id.Action == actionVacModal {
		b.handleVacationModal(ctx, i, id, data)
		return
	}
	if id.Action != actionNoteModal {
		return
	}
	g, l := b.guildAndLocale(ctx, id.GuildID)
	comment := modalValue(data, "comment")
	user := interactionUser(i)
	if _, err := b.api.RecordMood(ctx, id.GuildID, user.ID, id.Day, id.Mood, comment); err != nil {
		b.surveyError(i, l, g, id, err)
		return
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, answeredMessage(l, g, id, b.userName(id.GuildID, user), comment))
}

func modalValue(data discordgo.ModalSubmitInteractionData, field string) string {
	for _, c := range data.Components {
		row, ok := c.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, rc := range row.Components {
			if in, ok := rc.(*discordgo.TextInput); ok && in.CustomID == field {
				return in.Value
			}
		}
	}
	return ""
}

// --- Slash commands (in a server) ---

func (b *Bot) handleCommand(ctx context.Context, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name == menuKudos && i.GuildID != "" {
		b.cmdKudosMenu(i, b.userLocale(ctx, i), interactionUser(i))
		return
	}
	if data.Name != "happyx" || len(data.Options) == 0 || i.GuildID == "" {
		return
	}
	sub := data.Options[0]
	user := interactionUser(i)
	l := b.userLocale(ctx, i)
	switch sub.Name {
	case cmdStatus:
		b.cmdStatus(ctx, i, l, user)
	case cmdPause:
		err := b.api.RemoveMember(ctx, i.GuildID, user.ID)
		if err != nil && !errors.Is(err, api.ErrNotFound) {
			b.replyError(i, l, err)
			return
		}
		b.ephemeral(i, l.T("pause.done", "resume_cmd", b.resumeMention()))
	case cmdResume:
		if err := b.resume(ctx, i.GuildID, user); err != nil {
			b.replyError(i, l, err)
			return
		}
		b.ephemeral(i, l.T("resume.done"))
	case cmdVacation:
		b.cmdVacation(ctx, i, l, user, sub.Options)
	case cmdConfig:
		b.cmdConfig(ctx, i, l, user, sub.Options)
	case cmdReport:
		b.cmdReport(ctx, i, l, user, sub.Options)
	case cmdKudos:
		b.cmdKudos(ctx, i, l, user, sub.Options)
	case cmdSetup:
		b.cmdSetup(ctx, i, user)
	}
}

// cmdStatus tells members where they stand with this server's survey, with the same options as
// the survey's: vacation, pause or resume, and their evolution. Whoever can see the reports also
// gets the server's settings.
func (b *Bot) cmdStatus(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User) {
	g, err := b.api.Guild(ctx, i.GuildID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	members, err := b.api.Members(ctx, i.GuildID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	recipients, err := b.api.SurveyRecipients(ctx, i.GuildID, false)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	vacations, err := b.api.Vacations(ctx, i.GuildID, user.ID)
	if err != nil && !errors.Is(err, api.ErrNotFound) {
		b.replyError(i, l, err)
		return
	}
	vacationLines, away := vacationStatus(l, vacations, guildToday(g, b.now()))
	you, paused := l.T("status.you_paused"), true
	for _, m := range members {
		if m.DiscordID == user.ID {
			paused = false
			you = l.T("status.you_not_your_role") // opted in, but the survey roles leave them out
			if away {
				you = l.T("status.you_on_vacation") // the survey list leaves them out today
			}
		}
	}
	for _, m := range recipients {
		if m.DiscordID == user.ID {
			you = l.T("status.you_receiving")
		}
	}
	discordAdmin := i.Member != nil && i.Member.Permissions&discordgo.PermissionAdministrator != 0
	level, err := b.api.Access(ctx, i.GuildID, user.ID, discordAdmin)
	settings := err == nil && accessRank(level) >= accessRank(api.AccessView)
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource,
		b.statusMessage(l, g, you, b.surveyOffLine(l, g), vacationLines, paused, settings, len(recipients)))
}

// statusMessage is /happyx status: where the member stands, what admins see of their answers,
// their vacations and, for whoever can see the reports, the server's settings.
func (b *Bot) statusMessage(l *i18n.Locale, g api.Guild, you, offLine, vacationLines string, paused, settings bool, participants int) *discordgo.InteractionResponseData {
	text := l.T("options.text", "guild", g.Name, "schedule", surveySchedule(l, g)) + "\n" + you
	if offLine != "" {
		text += "\n" + offLine
	}
	text += "\n" + privacyLine(l, g) + vacationLines
	if settings {
		text += "\n\n" + l.T("status.settings",
			"frequency", l.T("freq."+g.ReportFrequency), "admin", "<@"+g.AdminDiscordID+">",
			"language", i18n.Get(g.Language).Name, "participants", participants) +
			reportDMLine(l, g) + "\n" + kudosChannelLine(l, g) + "\n" + gameLine(l, g)
	}
	return &discordgo.InteractionResponseData{
		Content:         text,
		Components:      b.optionsButtons(l, g, guildToday(g, b.now()).Format(isoDate), "", paused),
		Flags:           discordgo.MessageFlagsEphemeral,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

func (b *Bot) cmdConfig(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	// Choosing who receives the reports is for owners, like giving access on the dashboard.
	need := api.AccessManage
	for _, o := range opts {
		if o.Name == optAdmin {
			need = api.AccessOwner
		}
	}
	if !b.allowed(ctx, i, l, user, need) {
		return
	}
	var p api.GuildPatch
	for _, o := range opts {
		switch o.Name {
		case optAnonymous:
			v := o.BoolValue()
			p.Anonymous = &v
		case optHour:
			v := int(o.IntValue())
			p.SurveyHour = &v
		case optTimezone:
			v := strings.TrimSpace(o.StringValue())
			if _, err := time.LoadLocation(v); err != nil || v == "" || strings.EqualFold(v, "local") {
				b.ephemeral(i, l.T("config.bad_timezone", "timezone", v))
				return
			}
			p.Timezone = &v
		case optFrequency:
			v := o.StringValue()
			p.ReportFrequency = &v
		case optLanguage:
			v := o.StringValue()
			p.Language = &v
		case optAdmin:
			v := o.UserValue(nil).ID
			p.AdminDiscordID = &v
		case optKudosChan:
			v := o.ChannelValue(nil).ID
			p.KudosChannelID = &v
		case optGames:
			v := o.BoolValue()
			p.GamesEnabled = &v
		case optWorkDays:
			p.WorkingDays = presetDays(o.StringValue())
		case optSurvey:
			v := o.BoolValue()
			p.SurveyEnabled = &v
		}
	}
	g, err := b.api.UpdateGuild(ctx, i.GuildID, p)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	if p.Language != nil {
		l = i18n.Get(g.Language) // confirm in the language just chosen
	}
	s := configSaved(l, g, p)
	if p.SurveyEnabled != nil {
		s += "\n" + b.surveyNotice(ctx, l, g, true)
	} else if line := b.surveyOffLine(l, g); line != "" {
		s += "\n" + line
	}
	b.ephemeral(i, s)
}

// configSaved confirms the new settings. When the change turned anonymity off, it adds that
// earlier answers stay anonymous.
func configSaved(l *i18n.Locale, g api.Guild, p api.GuildPatch) string {
	s := l.T("config.saved",
		"days", workingDaysText(l, g.WorkingDays), "hour", fmt.Sprintf("%02d", g.SurveyHour), "timezone", g.Timezone,
		"frequency", l.T("freq."+g.ReportFrequency), "admin", "<@"+g.AdminDiscordID+">",
		"anonymous", yesNo(l, g.Anonymous), "language", i18n.Get(g.Language).Name) + reportDMLine(l, g) + "\n" + kudosChannelLine(l, g) + "\n" + gameLine(l, g)
	if p.Anonymous != nil && !*p.Anonymous {
		s += "\n" + l.T("config.now_public")
	}
	if p.GamesEnabled != nil && *p.GamesEnabled {
		s += "\n" + l.T("config.games_on")
	}
	return s
}

func (b *Bot) cmdReport(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	if !b.allowed(ctx, i, l, user, api.AccessView) {
		return
	}
	g, err := b.api.Guild(ctx, i.GuildID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	from, to := "", "" // the API defaults to the last completed period
	for _, o := range opts {
		if o.Name == optPeriod && o.StringValue() == periodCurrent {
			from, to = schedule.CurrentPeriod(schedule.Settings{Timezone: g.Timezone, ReportFrequency: g.ReportFrequency}, b.now())
		}
	}
	r, err := b.api.Report(ctx, i.GuildID, from, to)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	msg := reportMessage(l, g, r)
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, &discordgo.InteractionResponseData{
		Embeds: msg.Embeds, Files: msg.Files, Flags: discordgo.MessageFlagsEphemeral,
	})
}

// allowed checks the caller has at least the need level in HappyX (see api.Access), and
// answers them if not. The server's owner and its Administrators always have it.
func (b *Bot) allowed(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User, need string) bool {
	discordAdmin := i.Member != nil && i.Member.Permissions&discordgo.PermissionAdministrator != 0
	level, err := b.api.Access(ctx, i.GuildID, user.ID, discordAdmin)
	if err != nil {
		b.replyError(i, l, err)
		return false
	}
	if accessRank(level) < accessRank(need) {
		g, err := b.api.Guild(ctx, i.GuildID)
		if err != nil {
			b.replyError(i, l, err)
			return false
		}
		b.ephemeral(i, l.T("admin.only", "admin", "<@"+g.AdminDiscordID+">"))
		return false
	}
	return true
}

func accessRank(level string) int {
	switch level {
	case api.AccessView:
		return 1
	case api.AccessManage:
		return 2
	case api.AccessOwner:
		return 3
	}
	return 0
}

// resumeMention is a clickable /happyx resume, which Discord shows in each user's language.
func (b *Bot) resumeMention() string {
	if id := b.commandID.Load(); id != nil {
		return fmt.Sprintf("</happyx %s:%s>", cmdResume, *id)
	}
	return "`/happyx " + cmdResume + "`"
}

// --- Responses ---

func (b *Bot) respond(i *discordgo.InteractionCreate, t discordgo.InteractionResponseType, data *discordgo.InteractionResponseData) {
	if err := b.dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: t, Data: data}); err != nil {
		b.log.Error("interaction respond", "err", err)
	}
}

func (b *Bot) ephemeral(i *discordgo.InteractionCreate, content string) {
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, &discordgo.InteractionResponseData{
		Content: content, Flags: discordgo.MessageFlagsEphemeral,
		AllowedMentions: &discordgo.MessageAllowedMentions{}, // show mentions without pinging
	})
}

func (b *Bot) replyError(i *discordgo.InteractionCreate, l *i18n.Locale, err error) {
	var apiErr *api.Error
	msg := l.T("error.generic")
	switch {
	case errors.Is(err, api.ErrForbidden):
		msg = l.T("error.not_member", "resume_cmd", b.resumeMention())
	case errors.Is(err, api.ErrConflict):
		msg = l.T("error.inactive")
	case errors.As(err, &apiErr) && apiErr.Status == 400:
		// The API's reason is in English and for developers: logged, not shown.
		b.log.Warn("interaction refused by the API", "err", err)
		msg = l.T("error.invalid")
	default:
		b.log.Error("interaction failed", "err", err)
	}
	b.ephemeral(i, msg)
}

func yesNo(l *i18n.Locale, v bool) string {
	if v {
		return l.T("yes")
	}
	return l.T("no")
}

// surveyOffLine says nobody gets the survey yet, while the server hasn't started it.
func (b *Bot) surveyOffLine(l *i18n.Locale, g api.Guild) string {
	if g.SendsSurvey() {
		return ""
	}
	return l.T("status.survey_off", "command", b.setupMention())
}

// afterFirstLine puts line right under the first line of text (the survey's schedule), if any.
func afterFirstLine(text, line string) string {
	if line == "" {
		return text
	}
	first, rest, ok := strings.Cut(text, "\n")
	if !ok {
		return text + "\n" + line
	}
	return first + "\n" + line + "\n" + rest
}

// reportDMLine says the report is not sent by DM, when the admin chose to see it only on the web.
func reportDMLine(l *i18n.Locale, g api.Guild) string {
	if g.SendsReportDM() {
		return ""
	}
	return "\n" + l.T("report.dm_off")
}
