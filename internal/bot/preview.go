package bot

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// Preview is one message the bot sends, built from sample data with the real builders, so
// cmd/preview can show what users see without a Discord server.
type Preview struct {
	Title     string // what the scene shows
	Channel   string // where it appears, e.g. "DM" or "#general"
	Command   string // the slash command that triggered it, if any
	Ephemeral bool   // only the user who ran the command sees it
	Msg       *discordgo.MessageSend
}

// Sample IDs and names used by the previews. cmd/preview shows these IDs as names.
const (
	PreviewGuildID   = "100000000000000001"
	PreviewAdminID   = "300000000000000001"
	PreviewAnaID     = "200000000000000001"
	PreviewBeaID     = "200000000000000002"
	PreviewChannelID = "400000000000000001"
	// Images the previews point at; cmd/preview draws them as placeholders.
	PreviewGuildIcon = "https://cdn.example/guild.png"
	PreviewAvatarAna = "https://cdn.example/ana.png"
	PreviewAvatarBea = "https://cdn.example/bea.png"
	PreviewGeneralID = "400000000000000002"
)

// Previews builds every scene in language code.
func Previews(code string) []Preview {
	l := i18n.Get(code)
	g := api.Guild{DiscordID: PreviewGuildID, Name: "Equipo Demo", AdminDiscordID: PreviewAdminID, Anonymous: true,
		Timezone: "Europe/Madrid", SurveyHour: 13, ReportFrequency: "weekly", Language: code, KudosChannelID: PreviewChannelID,
		WorkingDays: presetDays("mon_fri")}
	b := &Bot{}
	public, no := g, false
	public.Anonymous = false
	sunToThu := g
	sunToThu.WorkingDays = presetDays("sun_thu")
	day := "2026-10-23"
	ana := surveyRecipient{ID: PreviewAnaID, Name: "Ana", Streak: 5}
	anaFirst := ana
	anaFirst.PolicyURL = "https://happyx.example.com/#/privacy-policy"
	custom := g
	custom.SurveyMessage = "👋 ¡Hola {{user.name}}! ¿Cómo te encuentras hoy, {{weekday}}? ({{guild.name}})\nLlevas {{user.streak}} días seguidos respondiendo 🔥"
	kudosMsg := "Gracias por echarme un cable con el despliegue del viernes, sin ti no llegábamos 🚀"
	link := "https://discord.com/channels/" + PreviewGuildID + "/" + PreviewChannelID + "/1"

	answered := answeredMessage(l, g, surveyID{Action: actionMood, GuildID: g.DiscordID, Day: day, Mood: 4}, "Ana", "")
	commented := answeredMessage(l, g, surveyID{Action: actionMood, GuildID: g.DiscordID, Day: day, Mood: 4}, "Ana", "Semana intensa pero buen ambiente")
	anaP := kudosPerson{ID: PreviewAnaID, Name: "Ana", Avatar: PreviewAvatarAna}
	beaP := kudosPerson{ID: PreviewBeaID, Name: "Bea", Avatar: PreviewAvatarBea}

	week := "2026-10-19"
	question := gameQuestion(l, week)
	gameLink := "https://happyx.example.com/#/game/" + PreviewGuildID
	games := g
	games.GamesEnabled = true
	yes := true

	text := func(s string) *discordgo.MessageSend { return &discordgo.MessageSend{Content: s} }
	data := func(d *discordgo.InteractionResponseData) *discordgo.MessageSend {
		return &discordgo.MessageSend{Content: d.Content, Embeds: d.Embeds, Components: d.Components}
	}

	return append([]Preview{
		{Title: "Daily survey", Channel: "DM", Msg: surveyMessage(l, g, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey with the admin's own message", Channel: "DM", Msg: surveyMessage(l, custom, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey until the first answer since the privacy policy's date", Channel: "DM", Msg: surveyMessage(l, g, day, anaFirst, PreviewGuildIcon)},
		{Title: "After answering", Channel: "DM", Msg: data(answered)},
		{Title: "After adding a comment", Channel: "DM", Msg: data(commented)},
		{Title: "Kudos card", Channel: "#kudos", Command: "/happyx kudos", Msg: kudosCard(l, anaP, beaP, kudosMsg, nil)},
		{Title: "Kudos card from a message (right-click › Apps)", Channel: "#kudos", Command: menuKudos,
			Msg: kudosCard(l, anaP, beaP, kudosMsg, &kudosOrigin{ChannelID: PreviewGeneralID, Link: link})},
		{Title: "Kudos confirmation", Channel: "#general", Command: "/happyx kudos", Ephemeral: true,
			Msg: &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{kudosSent(l, PreviewBeaID, PreviewChannelID)}}},
		{Title: "Kudos DM to the receiver", Channel: "DM", Msg: kudosDM(l, g, anaP, kudosMsg, link, "kudos")},
		{Title: "Kudos to yourself", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.no_self"))},
		{Title: "Kudos to a bot", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.no_bots"))},
		{Title: "Kudos daily limit", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.too_many"))},
		{Title: "Status", Channel: "#general", Command: "/happyx status", Ephemeral: true, Msg: text(l.T("status.text",
			"days", workingDaysText(l, g.WorkingDays), "hour", "13", "timezone", g.Timezone, "frequency", l.T("freq.weekly"), "admin", "<@"+PreviewAdminID+">",
			"anonymous", yesNo(l, true), "language", l.Name, "participants", 12, "you", l.T("status.receiving")) +
			"\n" + kudosChannelLine(l, g))},
		{Title: "Changing the working days", Channel: "#general", Command: "/happyx config working_days:" + l.T("days.sun_thu"), Ephemeral: true,
			Msg: text(configSaved(l, sunToThu, api.GuildPatch{WorkingDays: sunToThu.WorkingDays}))},
		{Title: "Turning anonymity off", Channel: "#general", Command: "/happyx config anonymous:False", Ephemeral: true,
			Msg: text(configSaved(l, public, api.GuildPatch{Anonymous: &no}))},
		{Title: "Pause", Channel: "#general", Command: "/happyx pause", Ephemeral: true, Msg: text(l.T("pause.done", "resume_cmd", b.resumeMention()))},
		{Title: "Weekly report", Channel: "DM", Msg: reportMessage(l, g, previewReport())},
		{Title: "Turning the game on", Channel: "#general", Command: "/happyx config games:True", Ephemeral: true,
			Msg: text(configSaved(l, games, api.GuildPatch{GamesEnabled: &yes}))},
		{Title: "«Who's who?» question (Monday)", Channel: "DM", Msg: gameQuestionMessage(l, g, api.GameRound{Week: week, Question: question}, gameLink)},
		{Title: "A question the team proposed (Monday)", Channel: "DM", Msg: gameQuestionMessage(l, g, api.GameRound{Week: week, Question: question, Proposed: true}, gameLink)},
		{Title: "After answering the game", Channel: "DM", Msg: data(gameAnsweredMessage(l, g, question, week, "Astronauta, y luego bombera 🚒", gameLink))},
		{Title: "Guessing is open (Thursday)", Channel: "DM", Msg: gameGuessMessage(l, g, api.GameRound{Week: week, Question: question, Answers: 7}, gameLink)},
		{Title: "Too few answers to guess (Thursday)", Channel: "DM", Msg: gameGuessMessage(l, g, api.GameRound{Week: week, Question: question, Answers: 2}, "")},
		{Title: "After proposing a question", Channel: "DM", Ephemeral: true, Msg: text(l.T("game.proposed_ok", "question", "¿Cuál fue tu primer concierto?"))},
		{Title: "Proposing before Thursday", Channel: "DM", Ephemeral: true, Msg: text(l.T("game.propose_closed"))},
		{Title: "Answering after Wednesday", Channel: "DM", Ephemeral: true, Msg: text(l.T("game.closed"))},
	}, setupPreviews(l, g)...)
}

// setupPreviews are the setup assistant's pages, the introduction and the test survey.
func setupPreviews(l *i18n.Locale, g api.Guild) []Preview {
	v2 := func(c []discordgo.MessageComponent) *discordgo.MessageSend {
		return &discordgo.MessageSend{Components: c, Flags: discordgo.MessageFlagsIsComponentsV2}
	}
	g.Name, g.KudosChannelID = "Equipo Producto", ""
	roles := []*discordgo.Role{
		{ID: PreviewGuildID, Name: "@everyone"},
		{ID: "500000000000000001", Name: "Desarrollo", Position: 4},
		{ID: "500000000000000002", Name: "Diseño", Position: 3},
		{ID: "500000000000000003", Name: "Producto", Position: 2},
		{ID: "500000000000000004", Name: "Invitados", Position: 1},
		{ID: "500000000000000005", Name: "HappyX", Position: 5, Managed: true},
	}
	channels := []api.Channel{
		{ID: PreviewGeneralID, Name: "general", CanPost: true},
		{ID: PreviewChannelID, Name: "kudos", CanPost: false},
		{ID: "400000000000000003", Name: "random", CanPost: true},
	}
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC) // a Monday morning
	d := setupData{Guild: g, Roles: roles, Channels: channels, Recipients: 28, WebURL: "https://happyx.example.com", Now: now}

	only := d
	only.Guild.SurveyRoles = api.RoleFilter{Mode: api.RolesOnly, RoleIDs: []string{"500000000000000001", "500000000000000002"}}
	only.Recipients = 16
	picking := d
	page2 := only
	page2.Guild.KudosChannelID, page2.Guild.GamesEnabled = PreviewChannelID, true
	choosing := page2
	choosing.present = (&Bot{}).presentSelect(l, choosing)
	done := page2
	done.Notice = l.T("setup.present_done", "channel", "general")

	named := page2.Guild
	named.Anonymous = false
	who := joinAnd(l, []string{"Desarrollo", "Diseño"})
	pause := "`/happyx " + cmdPause + "`"
	test := (&Bot{now: func() time.Time { return now }}).testSurveyFor(l, g, surveyRecipient{ID: PreviewAnaID, Name: "Ana"}, PreviewGuildIcon)
	answered := testAnswered(l, g, "Ana", 4)

	return []Preview{
		{Title: "Setup assistant, page 1 (DM to the owner when the bot joins)", Channel: "DM", Msg: v2(setupComponents(l, d, "1"))},
		{Title: "Setup: picking roles for «Only these roles»", Channel: "DM", Msg: v2(setupComponents(l, picking, "1o"))},
		{Title: "Setup: only some roles get the survey", Channel: "DM", Msg: v2(setupComponents(l, only, "1"))},
		{Title: "Setup, page 2 (kudos channel without permission, game on)", Channel: "DM", Msg: v2(setupComponents(l, page2, "2"))},
		{Title: "Setup: choosing where to introduce HappyX", Channel: "DM", Msg: v2(setupComponents(l, choosing, "2c"))},
		{Title: "Setup: HappyX introduced", Channel: "DM", Msg: v2(setupComponents(l, done, "2"))},
		{Title: "/happyx setup in a server", Channel: "#general", Command: "/happyx " + cmdSetup, Ephemeral: true, Msg: v2(setupComponents(l, d, "1"))},
		{Title: "Introduction (anonymous, only some roles, game on)", Channel: "#general", Msg: v2(introComponents(l, page2.Guild, who, "alex", pause))},
		{Title: "Introduction (with names, everyone)", Channel: "#general", Msg: v2(introComponents(l, named, l.T("intro.who_all"), "alex", pause))},
		{Title: "Test survey (Try the survey)", Channel: "#general", Ephemeral: true, Msg: test},
		{Title: "Test survey answered (nothing saved)", Channel: "#general", Ephemeral: true,
			Msg: &discordgo.MessageSend{Embeds: answered.Embeds, Components: answered.Components}},
	}
}

func previewReport() api.Report {
	r := api.Report{From: "2026-10-19", To: "2026-10-25", Anonymous: true, Responses: 41, Respondents: 9, Average: 3.7,
		Distribution: map[string]int{"1": 1, "2": 4, "3": 10, "4": 17, "5": 9},
		Messages: []api.ReportMsg{
			{Day: "2026-10-20", Mood: 2, Message: "Muchas reuniones seguidas, me cuesta concentrarme"},
			{Day: "2026-10-22", Mood: 5, Message: "¡Por fin salió la release!"},
			{Day: "2026-10-23", Mood: 4, Message: "Semana intensa pero buen ambiente"},
		},
		Kudos: api.KudosSummary{Count: 6, Givers: 5, Receivers: 4, Highlights: []api.Kudos{
			{Day: "2026-10-23", FromUsername: "ana", ToUsername: "bea", Message: "Gracias por echarme un cable con el despliegue del viernes, sin ti no llegábamos 🚀"},
			{Day: "2026-10-22", FromUsername: "carlos", ToUsername: "dani", Message: "Por la revisión tan detallada del PR"},
			{Day: "2026-10-21", FromUsername: "bea", ToUsername: "ana", Message: "Por organizar la retro y traer café ☕"},
		}},
	}
	// Monday to Friday: the API leaves out non-working days nobody answered on.
	for i, avg := range []float64{3.2, 2.9, 3.8, 4.3, 4.1} {
		r.Days = append(r.Days, api.ReportDay{Day: fmt.Sprintf("2026-10-%d", 19+i), Average: avg, Responses: 8})
	}
	return r
}
