package bot

import (
	"encoding/json"
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
	// OpenSelect shows the select with this placeholder open, with its options and their
	// descriptions, as Discord does once it is clicked.
	OpenSelect string
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
	day := "2026-10-23"
	g := api.Guild{DiscordID: PreviewGuildID, Name: "Equipo Demo", AdminDiscordID: PreviewAdminID, Anonymous: true,
		Timezone: "Europe/Madrid", SurveyHour: 13, ReportFrequency: "weekly", Language: code, KudosChannelID: PreviewChannelID,
		WorkingDays: presetDays("mon_fri"), TeamSummary: true}
	b := &Bot{webURL: "https://happyx.example.com", now: func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }}
	cmdID := "900000000000000001" // as once the commands are registered: mentions are clickable
	b.commandID.Store(&cmdID)
	public, no := g, false
	noTeamChannel := g
	noTeamChannel.KudosChannelID = ""
	public.Anonymous, public.NamedSince = false, "2026-10-24" // turned off today: names from tomorrow
	five, fivePeople := 5, g
	fivePeople.AnonymityThreshold = 5
	three, threePeople := 3, g
	threePeople.AnonymityThreshold = 3
	one, onePerson := 1, g
	onePerson.AnonymityThreshold = 1
	two, twoPeople := 2, g
	twoPeople.AnonymityThreshold = 2
	// The Monday after: names started on Saturday 24, as the survey above announced.
	namedDay := "2026-10-26"
	sunToThu := g
	sunToThu.WorkingDays = presetDays("sun_thu")
	ana := surveyRecipient{ID: PreviewAnaID, Name: "Ana", Streak: 5}
	anaFirst := ana
	anaFirst.PolicyURL = "https://happyx.example.com/#/privacy-policy"
	custom := g
	custom.SurveyMessage = "👋 ¡Hola {{user.name}}! ¿Cómo te encuentras hoy, {{weekday}}? ({{guild.name}})"
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

	withAudience := previewReport()
	withAudience.Audience = 12
	prevWeek := previewReport()
	prevWeek.Average, prevWeek.Distribution = 3.4, map[string]int{"2": 6, "3": 12, "4": 14, "5": 4}
	note := api.TeamNote{ID: 1, AuthorDiscordID: PreviewAdminID, AuthorUsername: "alex", From: "2026-10-19", To: "2026-10-25",
		Body:      "Gracias por responder. Desde el lunes, martes por la tarde sin reuniones, para tener un rato seguido de trabajo. Lo revisamos en dos semanas.",
		ChannelID: PreviewChannelID, MessageID: "600000000000000001"}
	told := answeredMessage(l, g, surveyID{Action: actionMood, GuildID: g.DiscordID, Day: "2026-10-26", Mood: 4}, "Ana", "")
	if row, ok := told.Components[0].(discordgo.ActionsRow); ok {
		row.Components = append(row.Components, discordgo.Button{Label: l.T("answer.told_button", "name", "alex"), Style: discordgo.LinkButton,
			URL: messageLink(g.DiscordID, note.ChannelID, note.MessageID)})
		told.Components[0] = row
	}
	yesterdayAna := ana
	yesterdayAna.Yesterday = true
	guess := &guessInSurvey{week: week, round: api.GameRound{Week: week, Question: question, Answers: 7}, link: gameLink}
	fewGuess := &guessInSurvey{week: week, round: api.GameRound{Week: week, Question: question, Answers: 2}}
	summaryReport := api.Report{Respondents: 9, Audience: 12, Kudos: api.KudosSummary{Count: 6, Receivers: 4}}

	text := func(s string) *discordgo.MessageSend { return &discordgo.MessageSend{Content: s} }
	data := func(d *discordgo.InteractionResponseData) *discordgo.MessageSend {
		return &discordgo.MessageSend{Content: d.Content, Embeds: d.Embeds, Components: d.Components}
	}

	return append([]Preview{
		{Title: "Daily survey", Channel: "DM", Msg: surveyMessage(l, g, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey the day anonymity is turned off (names from tomorrow)", Channel: "DM", Msg: surveyMessage(l, public, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey the first week with names", Channel: "DM", Msg: surveyMessage(l, public, namedDay, ana, PreviewGuildIcon)},
		{Title: "Daily survey when results show even if only 1 person answers", Channel: "DM", Msg: surveyMessage(l, onePerson, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey when results show from 2 people", Channel: "DM", Msg: surveyMessage(l, twoPeople, day, ana, PreviewGuildIcon)},
		{Title: "Options (from the survey) with results from 1 person", Channel: "DM", Ephemeral: true, Msg: data(b.optionsMessage(l, onePerson, day, "1"))},
		{Title: "Daily survey with the admin's own message", Channel: "DM", Msg: surveyMessage(l, custom, day, ana, PreviewGuildIcon)},
		{Title: "Daily survey until the first answer since the privacy policy's date", Channel: "DM", Msg: surveyMessage(l, g, day, anaFirst, PreviewGuildIcon)},
		{Title: "Options (from the survey)", Channel: "DM", Ephemeral: true, Msg: data(b.optionsMessage(l, g, day, "1"))},
		{Title: "Options › Pause", Channel: "DM", Ephemeral: true, Msg: data(pauseConfirm(l, g, surveyID{Action: actionPause, GuildID: g.DiscordID, Day: day, Msg: "1"}))},
		{Title: "Paused survey (replaces the card)", Channel: "DM", Msg: previewPaused(l, g, day)},
		{Title: "Paused (the options' reply)", Channel: "DM", Ephemeral: true, Msg: text(l.T("options.paused", "guild", g.Name))},
		{Title: "Vacation set from the options", Channel: "DM", Ephemeral: true,
			Msg: text(l.T("vacation.done", "from", discordDate("2026-12-20"), "to", discordDate("2027-01-06"), "resume_cmd", b.resumeMention()))},
		{Title: "Yesterday's survey, unanswered, once today's arrives", Channel: "DM", Msg: data(closedSurvey(l, g, "2026-10-22"))},
		{Title: "After answering", Channel: "DM", Msg: data(answered)},
		{Title: "After adding a comment", Channel: "DM", Msg: data(commented)},
		{Title: "Change answer (the comment stays)", Channel: "DM",
			Msg: data(changeMessage(l, g, day, ana, PreviewGuildIcon, answeredQuote(&discordgo.Message{Embeds: commented.Embeds})))},
		{Title: "Yesterday's survey, answered, once today's arrives", Channel: "DM", Msg: &discordgo.MessageSend{Embeds: commented.Embeds}},
		{Title: "Survey paused days ago, turned back on", Channel: "DM", Msg: data(b.resumedSurvey(l, g, "2026-10-12", &discordgo.User{ID: ana.ID, Username: "ana"}))},
		{Title: "After answering on the week's last survey day («Your week»)", Channel: "DM", Msg: data(addWeek(l, answeredMessage(l, g, surveyID{Action: actionMood, GuildID: g.DiscordID, Day: day, Mood: 4}, "Ana", ""),
			weekLine(l, workingWeek(g.WorkingDays, parseDay(day)), map[string]int{"2026-10-19": 3, "2026-10-20": 4, "2026-10-22": 5, "2026-10-23": 4}), b.evolutionURL(g.DiscordID)))},
		{Title: "Change answer after midnight (yesterday's survey)", Channel: "DM",
			Msg: data(changeMessage(l, g, "2026-10-22", yesterdayAna, PreviewGuildIcon, ""))},
		{Title: "Thursday's survey with «Who's who?»", Channel: "DM", Msg: withGame(l, g, surveyMessage(l, games, "2026-10-22", ana, PreviewGuildIcon), guess)},
		{Title: "Thursday's survey with «Who's who?» when too few answered", Channel: "DM", Msg: withGame(l, g, surveyMessage(l, games, "2026-10-22", ana, PreviewGuildIcon), fewGuess)},
		{Title: "After answering Thursday's survey", Channel: "DM", Msg: data(carriedGame(asMessageSend(withGame(l, g, surveyMessage(l, games, "2026-10-22", ana, PreviewGuildIcon), guess))).
			onto(answeredMessage(l, g, surveyID{Action: actionMood, GuildID: g.DiscordID, Day: "2026-10-22", Mood: 4}, "Ana", "")))},
		{Title: "Thursday's survey, unanswered, once Friday's arrives (the game stays until Sunday)", Channel: "DM", Msg: closedWithGame(l, g, withGame(l, g, surveyMessage(l, games, "2026-10-22", ana, PreviewGuildIcon), guess))},
		{Title: "The survey in the team channel, for whoever doesn't get DMs", Channel: "#kudos", Msg: surveyPost(l, g)},
		{Title: "«Answer today» pressed", Channel: "#kudos", Ephemeral: true, Msg: surveyMessage(l, g, day, ana, PreviewGuildIcon)},
		{Title: "«Answer today» pressed on Thursday, by someone who plays «Who's who?»", Channel: "#kudos", Ephemeral: true,
			Msg: withGame(l, g, surveyMessage(l, games, "2026-10-22", ana, PreviewGuildIcon), guess)},
		{Title: "«Answer today» before that day's survey", Channel: "#kudos", Ephemeral: true,
			Msg: text(l.T("post.none", "when", fmt.Sprintf("<t:%d:F>", time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC).Unix())))},
		{Title: "«Answer today» by someone who doesn't get the survey today", Channel: "#kudos", Ephemeral: true,
			Msg: text(l.T("post.not_yours", "guild", g.Name, "status_cmd", b.mention(cmdStatus)))},
		{Title: "/happyx pause in a DM, in two servers", Channel: "DM", Command: "/happyx pause", Ephemeral: true,
			Msg: data(serverPicker(l, cmdPause, []api.UserGuild{{Guild: g}, {Guild: api.Guild{DiscordID: "100000000000000002", Name: "Club de lectura"}}}))},
		{Title: "/happyx resume in a DM, in two servers, one paused", Channel: "DM", Command: "/happyx resume", Ephemeral: true,
			Msg: data(serverPicker(l, cmdResume, []api.UserGuild{{Guild: g}, {Guild: api.Guild{DiscordID: "100000000000000002", Name: "Club de lectura"}, Paused: true}}))},
		{Title: "/happyx pause in a DM, once the server is picked", Channel: "DM", Ephemeral: true,
			Msg: text(l.T("options.paused_cmd", "guild", g.Name, "resume_cmd", b.resumeMention()))},
		{Title: "/happyx status in a DM", Channel: "DM", Command: "/happyx status", Ephemeral: true,
			Msg: data(b.statusMessage(l, g, l.T("status.you_receiving"), "", "", false, false, 12, 0))},
		{Title: "/happyx config in a DM", Channel: "DM", Command: "/happyx config", Ephemeral: true, Msg: text(l.T("dm.server_only"))},
		{Title: "Resuming while on vacation", Channel: "#general", Command: "/happyx resume", Ephemeral: true,
			Msg: text(resumedText(l, g.Name, []api.Vacation{{From: "2026-10-20", To: "2026-10-30"}}))},
		{Title: "Pause from /happyx status (the survey card in the DM turns paused too)", Channel: "#general", Ephemeral: true, Msg: text(l.T("options.paused_cmd", "guild", g.Name, "resume_cmd", b.resumeMention()))},
		{Title: "Kudos card", Channel: "#kudos", Command: "/happyx kudos", Msg: kudosCard(l, anaP, beaP, kudosMsg, nil)},
		{Title: "Kudos card from a message (right-click › Apps)", Channel: "#kudos", Command: menuKudos,
			Msg: kudosCard(l, anaP, beaP, kudosMsg, &kudosOrigin{ChannelID: PreviewGeneralID, Link: link})},
		{Title: "Kudos confirmation", Channel: "#general", Command: "/happyx kudos", Ephemeral: true,
			Msg: &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{kudosSent(l, PreviewBeaID, PreviewChannelID)}}},
		{Title: "Kudos DM to the receiver", Channel: "DM", Msg: kudosDM(l, g, anaP, kudosMsg, link, "kudos")},
		{Title: "Kudos to yourself", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.no_self"))},
		{Title: "Kudos to a bot", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.no_bots"))},
		{Title: "Kudos daily limit", Channel: "#general", Command: "/happyx kudos", Ephemeral: true, Msg: text(l.T("kudos.too_many"))},
		{Title: "Status (a member)", Channel: "#general", Command: "/happyx status", Ephemeral: true,
			Msg: data(b.statusMessage(l, g, l.T("status.you_receiving"), "", "", false, false, 12, 0))},
		{Title: "Status (paused)", Channel: "#general", Command: "/happyx status", Ephemeral: true,
			Msg: data(b.statusMessage(l, g, l.T("status.you_paused"), "", "", true, false, 12, 0))},
		{Title: "Status (an admin)", Channel: "#general", Command: "/happyx status", Ephemeral: true,
			Msg: data(b.statusMessage(l, g, l.T("status.you_receiving"), "", "", false, true, 12, 0))},
		{Title: "Status (an admin), with people who have DMs closed and no team channel", Channel: "#general", Command: "/happyx status", Ephemeral: true,
			Msg: data(b.statusMessage(l, noTeamChannel, l.T("status.you_receiving"), "", "", false, true, 12, 2))},
		{Title: "Changing the working days", Channel: "#general", Command: "/happyx config working_days:" + l.T("days.sun_thu"), Ephemeral: true,
			Msg: text(configSaved(l, sunToThu, api.GuildPatch{WorkingDays: sunToThu.WorkingDays}, 0))},
		{Title: "Turning anonymity off", Channel: "#general", Command: "/happyx config anonymous:False", Ephemeral: true,
			Msg: text(configSaved(l, public, api.GuildPatch{Anonymous: &no}, 0))},
		{Title: "Pause", Channel: "#general", Command: "/happyx pause", Ephemeral: true, Msg: text(l.T("pause.done", "resume_cmd", b.resumeMention()))},
		{Title: "Vacation", Channel: "#general", Command: "/happyx vacation from:20/12 to:6/1", Ephemeral: true,
			Msg: text(l.T("vacation.done", "from", discordDate("2026-12-20"), "to", discordDate("2027-01-06"), "resume_cmd", b.resumeMention()))},
		{Title: "Vacation with a date it can't read", Channel: "#general", Command: "/happyx vacation from:mañana to:6/1", Ephemeral: true,
			Msg: text(l.T("vacation.bad_date", "date", "mañana"))},
		{Title: "Vacation an admin set on the web", Channel: "DM", Msg: text(l.T("vacation.notice", "admin", "<@"+PreviewAdminID+">", "server", g.Name,
			"from", discordDate("2026-12-20"), "to", discordDate("2027-01-06"), "link", "<https://happyx.example.com/#/me>"))},
		{Title: "Weekly report", Channel: "DM", Msg: reportMessage(l, g, previewReport())},
		{Title: "Weekly report with a day below the threshold", Channel: "DM", Msg: reportMessage(l, g, previewPartlyWithheld())},
		{Title: "Weekly report when no day reached the threshold", Channel: "DM", Msg: reportMessage(l, g, previewWithheld())},
		{Title: "Weekly report (with the week before, «Tell the team» and the dashboard)", Channel: "DM", Msg: reportMessageWith(l, g, withAudience, reportOptions{
			Tell: true, DashboardURL: "https://happyx.example.com/#/g/" + PreviewGuildID + "?p=last-week", Previous: &prevWeek})},
		{Title: "«Tell the team» published", Channel: "DM", Ephemeral: true, Msg: data(published(l, g.DiscordID, note, l.T("team.published", "channel", "<#"+PreviewChannelID+">"), true))},
		{Title: "«Tell the team» pressed by another admin once alex told the team", Channel: "DM", Ephemeral: true, Msg: data(published(l, g.DiscordID, note, l.T("team.told_by", "user", "<@"+PreviewAdminID+">", "channel", "<#"+PreviewChannelID+">"), false))},
		{Title: "What the team sees", Channel: "#kudos", Msg: teamCard(l, note)},
		{Title: "After answering, the survey day after an admin told the team", Channel: "DM", Msg: data(told)},
		{Title: "Monday summary", Channel: "#kudos", Msg: teamSummary(l, games, week, summaryReport, &api.GameRound{Week: week, Question: question, Phase: "finished", Answers: 7, MinAnswers: 4}, gameLink+"?history")},
		{Title: "Setting the fewest people", Channel: "#general", Command: "/happyx config min_people:5", Ephemeral: true,
			Msg: text(configSaved(l, fivePeople, api.GuildPatch{AnonymityThreshold: &five}, 3))},
		{Title: "Lowering the fewest people", Channel: "#general", Command: "/happyx config min_people:3", Ephemeral: true,
			Msg: text(configSaved(l, threePeople, api.GuildPatch{AnonymityThreshold: &three}, 5))},
		{Title: "Lowering the fewest people to 1", Channel: "#general", Command: "/happyx config min_people:1", Ephemeral: true,
			Msg: text(configSaved(l, onePerson, api.GuildPatch{AnonymityThreshold: &one}, 3))},
		{Title: "Lowering the fewest people to 2", Channel: "#general", Command: "/happyx config min_people:2", Ephemeral: true,
			Msg: text(configSaved(l, twoPeople, api.GuildPatch{AnonymityThreshold: &two}, 3))},
		{Title: "Turning the game on", Channel: "#general", Command: "/happyx config games:True", Ephemeral: true,
			Msg: text(configSaved(l, games, api.GuildPatch{GamesEnabled: &yes}, 0))},
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
	off := false
	g.SurveyEnabled = &off // a server the bot just joined
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
	on := true
	started := done
	started.Guild.SurveyEnabled = &on
	started.Notice = surveyStateNotice(l, started.Guild, now, false, false)

	named := page2.Guild
	named.Anonymous = false
	onePersonIntro := page2.Guild
	onePersonIntro.AnonymityThreshold = 1
	onePersonSetup := d
	onePersonSetup.Guild.AnonymityThreshold = 1
	who := joinAnd(l, []string{"Desarrollo", "Diseño"})
	pause := "`/happyx " + cmdPause + "`"
	test := (&Bot{now: func() time.Time { return now }}).testSurveyFor(l, g, surveyRecipient{ID: PreviewAnaID, Name: "Ana"}, PreviewGuildIcon)
	answered := testAnswered(l, g, "Ana", 4)

	return []Preview{
		{Title: "Setup assistant, page 1 (DM to the owner when the bot joins)", Channel: "DM", Msg: v2(setupComponents(l, d, "1"))},
		{Title: "Setup: choosing the fewest people (results even if 1 person answers)", Channel: "DM", Msg: v2(setupComponents(l, onePersonSetup, "1")), OpenSelect: l.T("setup.min_people")},
		{Title: "Setup: picking roles for «Only these roles»", Channel: "DM", Msg: v2(setupComponents(l, picking, "1o"))},
		{Title: "Setup: only some roles get the survey", Channel: "DM", Msg: v2(setupComponents(l, only, "1"))},
		{Title: "Setup, page 2 (kudos channel without permission, game on)", Channel: "DM", Msg: v2(setupComponents(l, page2, "2"))},
		{Title: "Setup: choosing where to introduce HappyX", Channel: "DM", Msg: v2(setupComponents(l, choosing, "2c"))},
		{Title: "Setup: HappyX introduced", Channel: "DM", Msg: v2(setupComponents(l, done, "2"))},
		{Title: "Setup: survey started", Channel: "DM", Msg: v2(setupComponents(l, started, "2"))},
		{Title: "/happyx setup in a server", Channel: "#general", Command: "/happyx " + cmdSetup, Ephemeral: true, Msg: v2(setupComponents(l, d, "1"))},
		{Title: "Introduction (anonymous, only some roles, game on)", Channel: "#general", Msg: v2(introComponents(l, page2.Guild, who, "alex", pause))},
		{Title: "Introduction (results even if 1 person answers)", Channel: "#general", Msg: v2(introComponents(l, onePersonIntro, l.T("intro.who_all"), "alex", pause))},
		{Title: "Introduction (with names, everyone)", Channel: "#general", Msg: v2(introComponents(l, named, l.T("intro.who_all"), "alex", pause))},
		{Title: "Test survey (Try the survey)", Channel: "#general", Ephemeral: true, Msg: test},
		{Title: "Test survey answered (nothing saved)", Channel: "#general", Ephemeral: true,
			Msg: &discordgo.MessageSend{Embeds: answered.Embeds, Components: answered.Components}},
	}
}

func previewReport() api.Report {
	r := api.Report{From: "2026-10-19", To: "2026-10-25", Anonymous: true, Responses: 41, Respondents: 9, Average: 3.7,
		Distribution:       map[string]int{"1": 1, "2": 4, "3": 10, "4": 17, "5": 9},
		AnonymityThreshold: 3,
		// Anonymous comments: only their text, in an order unrelated to the day.
		Messages: []api.ReportMsg{
			{Message: "Semana intensa pero buen ambiente"},
			{Message: "Muchas reuniones seguidas, me cuesta concentrarme"},
			{Message: "¡Por fin salió la release!"},
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

// previewPartlyWithheld is the weekly report where only two people answered on Tuesday: that
// day counts only in the answers, not in the average, the distribution or the comments.
func previewPartlyWithheld() api.Report {
	r := previewReport()
	r.Days[1] = api.ReportDay{Day: r.Days[1].Day, Responses: 2, Withheld: true}
	r.Responses = 34
	r.Distribution = map[string]int{"1": 1, "2": 3, "3": 8, "4": 13, "5": 7}
	r.Average = 3.7
	return r
}

// previewWithheld is a week where no day had answers from 3 people, the threshold.
func previewWithheld() api.Report {
	r := api.Report{From: "2026-10-19", To: "2026-10-25", Anonymous: true, AnonymityThreshold: 3, Withheld: true,
		Responses: 6, Respondents: 2, Distribution: map[string]int{}}
	for i := range 5 {
		r.Days = append(r.Days, api.ReportDay{Day: fmt.Sprintf("2026-10-%d", 19+i), Responses: 1 + i%2, Withheld: true})
	}
	return r
}

// previewPaused is the survey card once paused.
func previewPaused(l *i18n.Locale, g api.Guild, day string) *discordgo.MessageSend {
	e := pausedEdit(l, g, day, "1", "1")
	return &discordgo.MessageSend{Embeds: *e.Embeds, Components: *e.Components}
}

// asMessageSend is a message as Discord sends it back, for what a builder reads off a card.
func asMessageSend(m *discordgo.MessageSend) *discordgo.Message {
	var out discordgo.Message
	raw, _ := json.Marshal(map[string]any{"embeds": m.Embeds, "components": m.Components})
	_ = json.Unmarshal(raw, &out)
	return &out
}

// closedWithGame is a survey closed by the next one, built as closeOldSurveys does from the card
// Discord sends back.
func closedWithGame(l *i18n.Locale, g api.Guild, survey *discordgo.MessageSend) *discordgo.MessageSend {
	game := carriedGame(asMessageSend(survey))
	return &discordgo.MessageSend{
		Embeds:     append([]*discordgo.MessageEmbed{closedEmbed(l, g.Name, "2026-10-22")}, game.embeds...),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: game.row}},
	}
}
