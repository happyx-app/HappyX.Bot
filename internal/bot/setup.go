package bot

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
	"github.com/happyx-app/HappyX.Bot/internal/schedule"
)

// The setup assistant is one message, sent by DM to the owner when the bot joins a server and
// shown (only to them) by /happyx setup. Every select saves as soon as it changes, and the
// message is redrawn with the saved settings. It has two pages: the survey, then the report and
// extras. Selects are plain string selects: role and channel selects don't work in a DM, which
// has no server to list them from.

// setupID is the custom ID of a setup component: what it changes, for which server, and which
// page to redraw. Page "1" and "2" are the two pages; "1o"/"1e" and "2o"/"2e" are those pages
// while roles are being picked for an "only"/"except" filter that isn't saved yet (the API
// needs at least one role for it); "2c" is page 2 while choosing where to introduce HappyX.
type setupID struct {
	Action  string
	GuildID string
	Page    string
}

const setupPrefix = "hxs"

const (
	setupPage      = "page"      // button: go to Page
	setupLanguage  = "lang"      // select
	setupAnonymous = "anon"      // select
	setupHour      = "hour"      // select
	setupTimezone  = "tz"        // select
	setupDays      = "days"      // select
	setupWho       = "who"       // select: survey role filter mode
	setupRoles     = "roles"     // select: survey roles
	setupFrequency = "freq"      // select
	setupReportDM  = "dm"        // select
	setupKudos     = "kudos"     // select
	setupGames     = "games"     // select
	setupPlayers   = "players"   // select: game role filter mode
	setupGameRoles = "gameroles" // select: game roles
	setupTry       = "try"       // button: send a survey that isn't saved
	setupPresent   = "present"   // select: the channel to introduce HappyX in
	setupStart     = "start"     // button: start the survey
	setupStop      = "stop"      // button: turn the survey off
)

func (s setupID) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", setupPrefix, s.Action, s.GuildID, s.Page)
}

func parseSetupID(v string) (setupID, bool) {
	p := strings.Split(v, ":")
	if len(p) != 4 || p[0] != setupPrefix || !snowflake(p[2]) {
		return setupID{}, false
	}
	switch p[3] {
	case "1", "1o", "1e", "2", "2o", "2e", "2c":
	default:
		return setupID{}, false
	}
	return setupID{Action: p[1], GuildID: p[2], Page: p[3]}, true
}

// testID is a button of a survey sent to try it: the answer is shown but never saved.
type testID struct {
	GuildID string
	Mood    int
}

const testPrefix = "hxt"

func (t testID) String() string { return fmt.Sprintf("%s:%s:%d", testPrefix, t.GuildID, t.Mood) }

func parseTestID(v string) (testID, bool) {
	p := strings.Split(v, ":")
	if len(p) != 3 || p[0] != testPrefix || !snowflake(p[1]) {
		return testID{}, false
	}
	mood, err := strconv.Atoi(p[2])
	if err != nil || mood < 1 || mood > len(moodEmojis) {
		return testID{}, false
	}
	return testID{GuildID: p[1], Mood: mood}, true
}

// setupTimezones are offered in the time zone select; the guild's own is added if missing.
// Any other zone can be typed with /happyx config or set on the web.
var setupTimezones = []string{
	"Europe/Madrid", "Atlantic/Canary", "Europe/Lisbon", "Europe/London", "Europe/Paris", "Europe/Berlin",
	"Europe/Rome", "Europe/Athens", "America/Mexico_City", "America/Bogota", "America/Lima", "America/Santiago",
	"America/Argentina/Buenos_Aires", "America/Sao_Paulo", "America/New_York", "America/Chicago",
	"America/Denver", "America/Los_Angeles", "Asia/Tokyo", "Australia/Sydney", "UTC",
}

// maxOptions is Discord's limit of options in a select.
const maxOptions = 25

// setupData is what a page of the assistant shows besides the guild's settings.
type setupData struct {
	Guild      api.Guild
	Roles      []*discordgo.Role     // the server's roles, from the session state
	Channels   []api.Channel         // text channels in sidebar order, with CanPost
	Recipients int                   // who gets the survey now; -1 if unknown
	WebURL     string                // the dashboard, "" if not deployed
	Now        time.Time             //
	Notice     string                // a line to show at the end, e.g. after introducing HappyX
	present    *discordgo.SelectMenu // set on page 2c
}

// setupComponents draws a page of the assistant as a Components v2 container.
func setupComponents(l *i18n.Locale, d setupData, page string) []discordgo.MessageComponent {
	g := d.Guild
	id := func(action string) string { return setupID{Action: action, GuildID: g.DiscordID, Page: page}.String() }
	text := func(s string) discordgo.MessageComponent { return discordgo.TextDisplay{Content: s} }
	label := func(s string) discordgo.MessageComponent { return text("**" + s + "**") }
	row := func(c ...discordgo.MessageComponent) discordgo.MessageComponent {
		return discordgo.ActionsRow{Components: c}
	}
	sel := func(action, placeholder string, opts []discordgo.SelectMenuOption) discordgo.MessageComponent {
		return row(discordgo.SelectMenu{CustomID: id(action), Placeholder: placeholder, Options: opts})
	}
	sep := discordgo.Separator{}

	var c []discordgo.MessageComponent
	if page[0] == '1' {
		c = append(c,
			text("-# "+l.T("setup.page1")+"\n### "+l.T("setup.title", "guild", g.Name)+"\n"+l.T("setup.saves")+offHint(l, g)),
			sep,
			label(l.T("setup.language")), sel(setupLanguage, l.T("setup.language"), languageOptions(g.Language)),
			label(l.T("setup.answers")), sel(setupAnonymous, l.T("setup.answers"), []discordgo.SelectMenuOption{
				{Label: l.T("setup.anonymous"), Description: l.T("setup.anonymous_desc"), Value: "yes", Default: g.Anonymous},
				{Label: l.T("setup.named"), Description: l.T("setup.named_desc"), Value: "no", Default: !g.Anonymous},
			}),
			label(l.T("setup.hour")), sel(setupHour, l.T("setup.hour"), hourOptions(g.SurveyHour)),
			sel(setupTimezone, l.T("setup.timezone"), timezoneOptions(g.Timezone)),
			label(l.T("setup.days")), sel(setupDays, l.T("setup.days"), dayOptions(l, g.WorkingDays)),
			label(l.T("setup.who")),
		)
		mode := filterMode(g.SurveyRoles, page)
		c = append(c, sel(setupWho, l.T("setup.who"), modeOptions(l, "setup.roles_", mode)))
		if mode != api.RolesEveryone {
			c = append(c, roleSelect(l, id(setupRoles), d.Roles, g.SurveyRoles, g.DiscordID)...)
		}
		if d.Recipients == 1 {
			c = append(c, text("-# "+l.T("setup.recipients_one")))
		} else if d.Recipients >= 0 {
			c = append(c, text("-# "+l.T("setup.recipients", "n", d.Recipients)))
		}
		c = append(c, sep, row(
			discordgo.Button{Label: l.T("setup.try"), Style: discordgo.SecondaryButton, CustomID: id(setupTry)},
			discordgo.Button{Label: l.T("setup.next"), Style: discordgo.PrimaryButton, CustomID: setupID{Action: setupPage, GuildID: g.DiscordID, Page: "2"}.String()},
		))
	} else {
		c = append(c,
			text("-# "+l.T("setup.page2")),
			label(l.T("setup.report")),
			sel(setupFrequency, l.T("setup.report"), []discordgo.SelectMenuOption{
				{Label: l.T("setup.weekly"), Value: "weekly", Default: g.ReportFrequency != "monthly"},
				{Label: l.T("setup.monthly"), Value: "monthly", Default: g.ReportFrequency == "monthly"},
			}),
			sel(setupReportDM, l.T("setup.report"), []discordgo.SelectMenuOption{
				{Label: l.T("setup.dm_on"), Value: "yes", Default: g.SendsReportDM()},
				{Label: l.T("setup.dm_off"), Value: "no", Default: !g.SendsReportDM()},
			}),
			label(l.T("setup.kudos")),
			sel(setupKudos, l.T("setup.kudos"), kudosOptions(l, d.Channels, g.KudosChannelID)),
		)
		if ch, ok := findChannel(d.Channels, g.KudosChannelID); ok && !ch.CanPost {
			c = append(c, text(l.T("setup.kudos_cant", "channel", ch.Name)))
		}
		c = append(c, label(l.T("setup.game")), sel(setupGames, l.T("setup.game"), []discordgo.SelectMenuOption{
			{Label: l.T("setup.game_off"), Value: "no", Default: !g.GamesEnabled},
			{Label: l.T("setup.game_on"), Value: "yes", Default: g.GamesEnabled},
		}))
		if g.GamesEnabled {
			mode := filterMode(g.GameRoles, page)
			c = append(c, sel(setupPlayers, l.T("setup.game"), modeOptions(l, "setup.players_", mode)))
			if mode != api.RolesEveryone {
				c = append(c, roleSelect(l, id(setupGameRoles), d.Roles, g.GameRoles, g.DiscordID)...)
			}
		}
		c = append(c, sep)
		st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
		if !g.SendsSurvey() {
			c = append(c, text(l.T("setup.off")))
		} else if next, ok := schedule.NextSurvey(st, d.Now); ok {
			c = append(c, text(l.T("setup.next_survey", "date", longDate(l, next), "hour", fmt.Sprintf("%02d", g.SurveyHour))))
		}
		c = append(c, text("-# "+l.T("setup.web")))
		if d.present != nil {
			c = append(c, row(*d.present))
		}
		buttons := []discordgo.MessageComponent{
			discordgo.Button{Label: l.T("setup.back"), Style: discordgo.SecondaryButton, CustomID: setupID{Action: setupPage, GuildID: g.DiscordID, Page: "1"}.String()},
		}
		if d.present == nil {
			buttons = append(buttons, discordgo.Button{Label: l.T("setup.present"), Style: discordgo.SecondaryButton,
				CustomID: setupID{Action: setupPage, GuildID: g.DiscordID, Page: "2c"}.String()})
		}
		if g.SendsSurvey() {
			buttons = append(buttons, discordgo.Button{Label: l.T("setup.stop"), Style: discordgo.SecondaryButton, CustomID: id(setupStop)})
		} else {
			buttons = append(buttons, discordgo.Button{Label: l.T("setup.start"), Style: discordgo.SuccessButton, CustomID: id(setupStart)})
		}
		if d.WebURL != "" {
			buttons = append(buttons, discordgo.Button{Label: l.T("setup.open_web"), Style: discordgo.LinkButton, URL: d.WebURL})
		}
		c = append(c, row(buttons...))
	}
	if d.Notice != "" {
		c = append(c, text(d.Notice))
	}
	color := surveyColor
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: c}}
}

// offHint tells the admin, on the first page, that the survey waits for them to start it.
func offHint(l *i18n.Locale, g api.Guild) string {
	if g.SendsSurvey() {
		return ""
	}
	return "\n" + l.T("setup.off_hint")
}

// surveyNotice confirms the survey was started or turned off, asking the API whether today's
// survey already went out when it would be due now.
func (b *Bot) surveyNotice(ctx context.Context, l *i18n.Locale, g api.Guild, withDate bool) string {
	now := b.now()
	sentToday := false
	st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
	if day, due := schedule.SurveyDue(st, now); due && g.SendsSurvey() {
		sent, err := b.api.DispatchSent(ctx, g.DiscordID, "survey", day)
		if err != nil {
			b.log.Warn("dispatch sent", "guild", g.DiscordID, "err", err)
			return l.T("setup.on") // unknown: say only that it's on
		}
		sentToday = sent
	}
	return surveyStateNotice(l, g, now, withDate, sentToday)
}

// surveyStateNotice confirms the survey was started or turned off. A survey started after its
// hour on a working day goes out right away, unless today's already went out (sentToday).
// withDate adds when the next one comes, for replies that don't show the next survey already.
func surveyStateNotice(l *i18n.Locale, g api.Guild, now time.Time, withDate, sentToday bool) string {
	if !g.SendsSurvey() {
		return l.T("setup.stopped")
	}
	st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
	if _, due := schedule.SurveyDue(st, now); due && !sentToday {
		return l.T("setup.started_now")
	}
	if !withDate {
		return l.T("setup.on")
	}
	if next, ok := schedule.NextSurvey(st, now); ok {
		return l.T("setup.started", "date", longDate(l, next), "hour", fmt.Sprintf("%02d", g.SurveyHour))
	}
	return ""
}

// filterMode is the filter's mode, or the one being picked when the page says so.
func filterMode(f api.RoleFilter, page string) string {
	switch {
	case strings.HasSuffix(page, "o"):
		return api.RolesOnly
	case strings.HasSuffix(page, "e"):
		return api.RolesExcept
	case f.Everyone():
		return api.RolesEveryone
	}
	return f.Mode
}

func modeOptions(l *i18n.Locale, prefix, mode string) []discordgo.SelectMenuOption {
	return []discordgo.SelectMenuOption{
		{Label: l.T(prefix + "all"), Value: api.RolesEveryone, Default: mode == api.RolesEveryone},
		{Label: l.T(prefix + "only"), Value: api.RolesOnly, Default: mode == api.RolesOnly},
		{Label: l.T(prefix + "except"), Value: api.RolesExcept, Default: mode == api.RolesExcept},
	}
}

// roleSelect picks the roles of a filter: the server's roles, highest first, without @everyone
// and the roles of bots. The chosen ones always fit, so saving never drops a role unseen.
func roleSelect(l *i18n.Locale, customID string, roles []*discordgo.Role, f api.RoleFilter, guildID string) []discordgo.MessageComponent {
	chosen := map[string]bool{}
	for _, id := range f.RoleIDs {
		chosen[id] = true
	}
	var picked, rest []*discordgo.Role
	for _, r := range roles {
		if r.ID == guildID || r.Managed {
			continue
		}
		if chosen[r.ID] {
			picked = append(picked, r)
		} else {
			rest = append(rest, r)
		}
	}
	byPosition := func(a, b *discordgo.Role) int { return cmp.Compare(b.Position, a.Position) }
	slices.SortFunc(picked, byPosition)
	slices.SortFunc(rest, byPosition)
	all := append(picked, rest...)
	more := len(all) > maxOptions
	if more {
		all = all[:maxOptions]
	}
	if len(all) == 0 {
		return nil
	}
	opts := make([]discordgo.SelectMenuOption, 0, len(all))
	for _, r := range all {
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(r.Name, 100), Value: r.ID, Default: chosen[r.ID]})
	}
	zero := 0
	out := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{
		CustomID: customID, Placeholder: l.T("setup.pick_roles"), MinValues: &zero, MaxValues: len(opts), Options: opts,
	}}}}
	if more {
		out = append(out, discordgo.TextDisplay{Content: "-# " + l.T("setup.more_roles")})
	}
	return out
}

func languageOptions(current string) []discordgo.SelectMenuOption {
	var opts []discordgo.SelectMenuOption
	for _, code := range i18n.Codes() {
		opts = append(opts, discordgo.SelectMenuOption{Label: i18n.Get(code).Name, Value: code, Default: code == current})
	}
	return opts
}

func hourOptions(current int) []discordgo.SelectMenuOption {
	opts := make([]discordgo.SelectMenuOption, 0, 24)
	for h := 0; h < 24; h++ {
		opts = append(opts, discordgo.SelectMenuOption{Label: fmt.Sprintf("%02d:00", h), Value: strconv.Itoa(h), Default: h == current})
	}
	return opts
}

func timezoneOptions(current string) []discordgo.SelectMenuOption {
	zones := slices.Clone(setupTimezones)
	if current != "" && !slices.Contains(zones, current) {
		zones = append([]string{current}, zones...)
	}
	opts := make([]discordgo.SelectMenuOption, 0, len(zones))
	for _, z := range zones {
		opts = append(opts, discordgo.SelectMenuOption{Label: z, Value: z, Default: z == current})
	}
	return opts
}

// dayOptions are the presets of /happyx config; days set on the web that match none show as
// one more option, so the select doesn't claim a preset that isn't saved.
func dayOptions(l *i18n.Locale, current []int) []discordgo.SelectMenuOption {
	if len(current) == 0 {
		current = presetDays("mon_fri")
	}
	var opts []discordgo.SelectMenuOption
	matched := false
	for _, p := range workingDayPresets {
		same := sameDays(p.days, current)
		matched = matched || same
		opts = append(opts, discordgo.SelectMenuOption{Label: l.T("days." + p.key), Value: p.key, Default: same})
	}
	if !matched {
		opts = append(opts, discordgo.SelectMenuOption{Label: l.T("setup.days_custom"), Value: "custom", Default: true})
	}
	return opts
}

func sameDays(a, b []int) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// kudosOptions are "where the kudos are given" and the channels, those the bot can post in
// first. The chosen channel always fits.
func kudosOptions(l *i18n.Locale, channels []api.Channel, current string) []discordgo.SelectMenuOption {
	opts := []discordgo.SelectMenuOption{{Label: l.T("setup.kudos_here"), Value: "-", Default: current == ""}}
	for _, ch := range channelOptions(channels, current, maxOptions-1) {
		opts = append(opts, discordgo.SelectMenuOption{Label: "#" + truncate(ch.Name, 99), Value: ch.ID, Default: ch.ID == current})
	}
	return opts
}

// channelOptions picks up to n channels: current (if any), then those the bot can post in.
func channelOptions(channels []api.Channel, current string, n int) []api.Channel {
	var out []api.Channel
	if ch, ok := findChannel(channels, current); ok {
		out = append(out, ch)
	}
	for _, ch := range channels {
		if len(out) == n {
			break
		}
		if ch.CanPost && ch.ID != current {
			out = append(out, ch)
		}
	}
	return out
}

func findChannel(channels []api.Channel, id string) (api.Channel, bool) {
	if id == "" {
		return api.Channel{}, false
	}
	for _, ch := range channels {
		if ch.ID == id {
			return ch, true
		}
	}
	return api.Channel{}, false
}

// setupPatch turns a select's choice into the settings change it makes. page is the page to
// draw next, which differs when a filter mode waits for its roles.
// save is false when nothing is saved yet.
func setupPatch(id setupID, values []string) (p api.GuildPatch, page string, save, ok bool) {
	page = id.Page
	v := ""
	if len(values) > 0 {
		v = values[0]
	}
	yes := v == "yes"
	switch id.Action {
	case setupLanguage:
		p.Language = &v
	case setupAnonymous:
		p.Anonymous = &yes
	case setupHour:
		h, err := strconv.Atoi(v)
		if err != nil || h < 0 || h > 23 {
			return p, page, false, false
		}
		p.SurveyHour = &h
	case setupTimezone:
		p.Timezone = &v
	case setupDays:
		if p.WorkingDays = presetDays(v); p.WorkingDays == nil {
			return p, page, false, false // "custom" is what's saved already
		}
	case setupWho, setupPlayers:
		base := page[:1]
		switch v {
		case api.RolesOnly, api.RolesExcept:
			return p, base + v[:1], false, true // wait for the roles
		case api.RolesEveryone:
		default:
			return p, page, false, false
		}
		f := &api.RoleFilter{Mode: api.RolesEveryone, RoleIDs: []string{}}
		if id.Action == setupWho {
			p.SurveyRoles = f
		} else {
			p.GameRoles = f
		}
		page = base
	case setupRoles, setupGameRoles:
		f := &api.RoleFilter{Mode: api.RolesEveryone, RoleIDs: []string{}}
		if len(values) > 0 {
			f = &api.RoleFilter{Mode: api.RolesOnly, RoleIDs: values}
			if strings.HasSuffix(page, "e") {
				f.Mode = api.RolesExcept
			}
		}
		if id.Action == setupRoles {
			p.SurveyRoles = f
		} else {
			p.GameRoles = f
		}
		page = page[:1]
	case setupFrequency:
		if v != "weekly" && v != "monthly" {
			return p, page, false, false
		}
		p.ReportFrequency = &v
	case setupReportDM:
		p.ReportDM = &yes
	case setupKudos:
		if v == "-" {
			v = ""
		}
		p.KudosChannelID = &v
	case setupGames:
		p.GamesEnabled = &yes
	case setupStart, setupStop:
		on := id.Action == setupStart
		p.SurveyEnabled = &on
	default:
		return p, page, false, false
	}
	return p, page, true, true
}

// --- Handlers ---

// setupFlags are the flags of the assistant's message: Components v2, and only visible to the
// admin when shown in a server.
func setupFlags(i *discordgo.InteractionCreate) discordgo.MessageFlags {
	if i.GuildID != "" {
		return discordgo.MessageFlagsIsComponentsV2 | discordgo.MessageFlagsEphemeral
	}
	return discordgo.MessageFlagsIsComponentsV2
}

// cmdSetup shows the assistant to an admin, only to them.
func (b *Bot) cmdSetup(ctx context.Context, i *discordgo.InteractionCreate, user *discordgo.User) {
	g, l := b.guildAndLocale(ctx, i.GuildID)
	if !b.allowed(ctx, i, l, user, api.AccessManage) {
		return
	}
	d, err := b.setupData(ctx, i.GuildID, g)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, &discordgo.InteractionResponseData{
		Flags: setupFlags(i), Components: setupComponents(l, d, "1"),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
}

// sendSetup sends the assistant to the server's owner by DM, right after the bot joins.
func (b *Bot) sendSetup(ctx context.Context, guildID, ownerID string) {
	g, l := b.guildAndLocale(ctx, guildID)
	if g.DiscordID == "" {
		return
	}
	d, err := b.setupData(ctx, guildID, g)
	if err != nil {
		b.log.Error("setup data", "guild", guildID, "err", err)
		return
	}
	ch, err := b.dg.UserChannelCreate(ownerID, discordgo.WithContext(ctx))
	if err == nil {
		_, err = b.dg.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
			Flags: discordgo.MessageFlagsIsComponentsV2, Components: setupComponents(l, d, "1"),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}, discordgo.WithContext(ctx))
	}
	if err != nil {
		b.log.Warn("send setup to owner", "guild", guildID, "err", err) // DMs closed: /happyx setup still works
	}
}

func (b *Bot) setupData(ctx context.Context, guildID string, g api.Guild) (setupData, error) {
	d := setupData{Guild: g, Recipients: -1, WebURL: b.webURL, Now: b.now()}
	if gs, err := b.dg.State.Guild(guildID); err == nil {
		d.Roles = gs.Roles
		d.Channels = channelList(gs.Channels, b.canPost)
	}
	if r, err := b.api.SurveyRecipients(ctx, guildID, false); err == nil {
		d.Recipients = len(r)
	}
	return d, nil
}

// canPost reports whether the bot may post the kudos card (or the introduction) in a channel.
func (b *Bot) canPost(channelID string) bool {
	me := b.dg.State.User
	if me == nil {
		return true
	}
	perms, err := b.dg.State.UserChannelPermissions(me.ID, channelID)
	if err != nil {
		return true // unknown: let Discord answer
	}
	return perms&kudosPermissions == kudosPermissions
}

// setupAllowed checks the user may change the server's settings. In a DM there is no member,
// so the server's Administrators who aren't HappyX admins are told to use /happyx setup.
func (b *Bot) setupAllowed(ctx context.Context, i *discordgo.InteractionCreate, guildID string, l *i18n.Locale) bool {
	user := interactionUser(i)
	discordAdmin := i.Member != nil && i.Member.Permissions&discordgo.PermissionAdministrator != 0
	level, err := b.api.Access(ctx, guildID, user.ID, discordAdmin)
	if err != nil {
		b.replyError(i, l, err)
		return false
	}
	if accessRank(level) < accessRank(api.AccessManage) {
		g, _ := b.api.Guild(ctx, guildID)
		b.ephemeral(i, l.T("admin.only", "admin", "<@"+g.AdminDiscordID+">"))
		return false
	}
	return true
}

func (b *Bot) handleSetup(ctx context.Context, i *discordgo.InteractionCreate, id setupID) {
	g, l := b.guildAndLocale(ctx, id.GuildID)
	if g.DiscordID == "" {
		b.replyError(i, l, fmt.Errorf("guild %s not found", id.GuildID))
		return
	}
	if !b.setupAllowed(ctx, i, id.GuildID, l) {
		return
	}
	values := i.MessageComponentData().Values
	page := id.Page
	notice := ""
	switch id.Action {
	case setupPage:
	case setupTry:
		b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, b.testSurvey(l, g, interactionUser(i), i.GuildID != ""))
		return
	case setupPresent:
		page = "2"
		notice = b.present(ctx, l, g, values)
	default:
		p, next, save, ok := setupPatch(id, values)
		if !ok {
			b.respond(i, discordgo.InteractionResponseDeferredMessageUpdate, nil)
			return
		}
		page = next
		if save {
			updated, err := b.api.UpdateGuild(ctx, id.GuildID, p)
			if err != nil {
				b.replyError(i, l, err)
				return
			}
			g = updated
			if p.Language != nil {
				l = i18n.Get(g.Language) // redraw in the language just chosen
			}
			if p.SurveyEnabled != nil {
				notice = b.surveyNotice(ctx, l, g, false)
			}
		}
	}
	d, _ := b.setupData(ctx, id.GuildID, g)
	d.Notice = notice
	if page == "2c" {
		d.present = b.presentSelect(l, d)
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
		Flags: discordgo.MessageFlagsIsComponentsV2, Components: setupComponents(l, d, page),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
}

// presentSelect offers the channels the bot can post in, to introduce HappyX there.
func (b *Bot) presentSelect(l *i18n.Locale, d setupData) *discordgo.SelectMenu {
	var opts []discordgo.SelectMenuOption
	for _, ch := range channelOptions(d.Channels, "", maxOptions) {
		opts = append(opts, discordgo.SelectMenuOption{Label: "#" + truncate(ch.Name, 99), Value: ch.ID})
	}
	if len(opts) == 0 {
		return nil
	}
	return &discordgo.SelectMenu{
		CustomID:    setupID{Action: setupPresent, GuildID: d.Guild.DiscordID, Page: "2c"}.String(),
		Placeholder: l.T("setup.present_where"), Options: opts,
	}
}

// present posts the introduction in the chosen channel and says how it went.
func (b *Bot) present(ctx context.Context, l *i18n.Locale, g api.Guild, values []string) string {
	if len(values) != 1 || !snowflake(values[0]) {
		return ""
	}
	c, err := b.dg.State.Channel(values[0])
	if err != nil || c.GuildID != g.DiscordID {
		return "" // only a channel of this server
	}
	name := c.Name
	_, err = b.dg.ChannelMessageSendComplex(values[0], &discordgo.MessageSend{
		Flags: discordgo.MessageFlagsIsComponentsV2, Components: b.introComponents(l, g),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx))
	if err != nil {
		b.log.Warn("introduce HappyX", "guild", g.DiscordID, "err", err)
		return l.T("setup.present_cant", "channel", name)
	}
	return l.T("setup.present_done", "channel", name)
}

// introComponents is the message that presents HappyX to the team: when and how the survey
// comes, who sees what, the game if it's on, how to stop it, and a button to try it.
func (b *Bot) introComponents(l *i18n.Locale, g api.Guild) []discordgo.MessageComponent {
	return introComponents(l, g, b.whoText(l, g), b.displayName(g.DiscordID, api.Member{DiscordID: g.AdminDiscordID}), b.pauseMention())
}

// introComponents is the introduction with the names already resolved: who gets the survey, the
// main admin and the /happyx pause command.
func introComponents(l *i18n.Locale, g api.Guild, who, admin, pause string) []discordgo.MessageComponent {
	privacy := "intro.anonymous"
	if !g.Anonymous {
		privacy = "intro.named"
	}
	lines := []string{
		"### " + l.T("intro.title"),
		l.T("intro.survey", "days", daysLabel(l, g.WorkingDays), "hour", fmt.Sprintf("%02d", g.SurveyHour),
			"timezone", g.Timezone, "who", who, "question", l.T("survey.question")),
		l.T(privacy, "admin", admin),
	}
	if g.GamesEnabled {
		lines = append(lines, l.T("intro.game"))
	}
	lines = append(lines, l.T("intro.pause", "cmd", pause))
	color := surveyColor
	return []discordgo.MessageComponent{discordgo.Container{AccentColor: &color, Components: []discordgo.MessageComponent{
		discordgo.TextDisplay{Content: strings.Join(lines, "\n\n")},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{
			Label: l.T("setup.try"), Style: discordgo.PrimaryButton, CustomID: testID{GuildID: g.DiscordID}.tryString(),
		}}},
	}}}
}

// tryString is the custom ID of the introduction's "Try the survey" button, which anyone in the
// server may press.
func (t testID) tryString() string { return fmt.Sprintf("%s:%s:try", testPrefix, t.GuildID) }

func parseTryID(v string) (string, bool) {
	p := strings.Split(v, ":")
	if len(p) != 3 || p[0] != testPrefix || p[2] != "try" || !snowflake(p[1]) {
		return "", false
	}
	return p[1], true
}

// daysLabel names the working days the long way when they are a preset, like «De lunes a
// viernes», and the short way otherwise.
func daysLabel(l *i18n.Locale, days []int) string {
	if len(days) == 0 {
		days = presetDays("mon_fri")
	}
	for _, p := range workingDayPresets {
		if sameDays(p.days, days) {
			return l.T("days." + p.key)
		}
	}
	return workingDaysText(l, days)
}

// whoText says who gets the survey: everyone, the chosen roles, or everyone but them.
func (b *Bot) whoText(l *i18n.Locale, g api.Guild) string {
	f := g.SurveyRoles
	if f.Everyone() || len(f.RoleIDs) == 0 {
		return l.T("intro.who_all")
	}
	var names []string
	for _, id := range f.RoleIDs {
		if r, err := b.dg.State.Role(g.DiscordID, id); err == nil {
			names = append(names, r.Name)
		}
	}
	list := joinAnd(l, names)
	if f.Mode == api.RolesExcept {
		return l.T("intro.who_except", "roles", list)
	}
	return list
}

func joinAnd(l *i18n.Locale, names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + l.T("intro.and") + names[len(names)-1]
}

// pauseMention is a clickable /happyx pause, which Discord shows in each user's language.
// setupMention is /happyx setup as a clickable command when its ID is known.
func (b *Bot) setupMention() string {
	if id := b.commandID.Load(); id != nil {
		return fmt.Sprintf("</happyx %s:%s>", cmdSetup, *id)
	}
	return "`/happyx " + cmdSetup + "`"
}

func (b *Bot) pauseMention() string {
	if id := b.commandID.Load(); id != nil {
		return fmt.Sprintf("</happyx %s:%s>", cmdPause, *id)
	}
	return "`/happyx " + cmdPause + "`"
}

// testSurvey is the daily survey as this server sends it, with buttons that save nothing.
// In a server only the person who pressed sees it.
func (b *Bot) testSurvey(l *i18n.Locale, g api.Guild, user *discordgo.User, ephemeral bool) *discordgo.InteractionResponseData {
	msg := b.testSurveyFor(l, g, surveyRecipient{ID: user.ID, Name: b.userName(g.DiscordID, user)}, b.guildIcon(g.DiscordID))
	data := &discordgo.InteractionResponseData{Embeds: msg.Embeds, Components: msg.Components, AllowedMentions: msg.AllowedMentions}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	return data
}

// testSurveyFor is the survey to try: today's survey for to, with test buttons and a footer
// that says nothing is saved.
func (b *Bot) testSurveyFor(l *i18n.Locale, g api.Guild, to surveyRecipient, icon string) *discordgo.MessageSend {
	msg := surveyMessage(l, g, b.now().Format(schedule.DateLayout), to, icon)
	buttons := make([]discordgo.MessageComponent, 0, len(moodEmojis))
	for i, emoji := range moodEmojis {
		buttons = append(buttons, discordgo.Button{
			Emoji: &discordgo.ComponentEmoji{Name: emoji}, Style: discordgo.SecondaryButton,
			CustomID: testID{GuildID: g.DiscordID, Mood: i + 1}.String(),
		})
	}
	msg.Embeds[0].Footer.Text += " · " + l.T("test.note")
	msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
	return msg
}

// testAnswered thanks for a test answer like the real survey does, and says it wasn't saved.
func testAnswered(l *i18n.Locale, g api.Guild, name string, mood int) *discordgo.InteractionResponseData {
	answered := answeredMessage(l, g, surveyID{GuildID: g.DiscordID, Mood: mood}, name, "")
	answered.Embeds[0].Description = "### " + l.T("answer.thanks", "emoji", moodEmoji(mood), "name", name)
	answered.Embeds[0].Footer = &discordgo.MessageEmbedFooter{Text: l.T("test.note")}
	answered.Components = []discordgo.MessageComponent{}
	return answered
}

// handleTestButton answers the introduction's "Try the survey" and the test survey's buttons.
// It reports whether customID was one of them.
func (b *Bot) handleTestButton(ctx context.Context, i *discordgo.InteractionCreate, customID string) bool {
	if guildID, ok := parseTryID(customID); ok {
		g, l := b.guildAndLocale(ctx, guildID)
		if g.DiscordID == "" || i.GuildID != guildID {
			return true
		}
		b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, b.testSurvey(l, g, interactionUser(i), true))
		return true
	}
	id, ok := parseTestID(customID)
	if !ok {
		return false
	}
	g, l := b.guildAndLocale(ctx, id.GuildID)
	b.respond(i, discordgo.InteractionResponseUpdateMessage, testAnswered(l, g, b.userName(id.GuildID, interactionUser(i)), id.Mood))
	return true
}
