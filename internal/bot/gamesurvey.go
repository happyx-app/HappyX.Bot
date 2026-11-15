package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
	"github.com/happyx-app/HappyX.Bot/internal/schedule"
)

// «Who's who?» on Thursday rides on the survey: instead of a DM of its own, that day's survey
// carries one more line and one button, to guess on the web or, when the round got too few
// answers, to propose next week's question. The line stays on the card when it's answered, and
// when the next survey closes it, since guessing lasts until Sunday. Whoever plays the game but
// doesn't get the survey still gets the game's own DM.

// guessInSurvey is what a survey day carries about the game: the round, the dashboard's game
// page to guess on ("" when there is nothing to guess), and who plays.
type guessInSurvey struct {
	week    string
	round   api.GameRound
	link    string
	players []api.Member
}

// claimGuess takes over the game's guessing DM for today's survey, when it is due today. It
// returns nil when the game isn't on, isn't due, was sent already, or can't be read; the game's
// own dispatch then sends it as before.
func (b *Bot) claimGuess(ctx context.Context, g api.Guild) *guessInSurvey {
	if !g.GamesEnabled {
		return nil
	}
	st := schedule.Settings{Timezone: g.Timezone, SurveyHour: g.SurveyHour, WorkingDays: g.WorkingDays}
	week, due := schedule.GameGuessDue(st, b.now())
	if !due {
		return nil
	}
	link := b.gameURL(g.DiscordID)
	if link == "" {
		return nil // without the dashboard the line has nothing to link: the game's DM asks for proposals
	}
	r, err := b.api.GameRound(ctx, g.DiscordID, week)
	if err != nil {
		if !errors.Is(err, api.ErrNotFound) {
			b.log.Warn("game round for the survey", "guild", g.DiscordID, "err", err)
		}
		return nil
	}
	players, err := b.api.GamePlayers(ctx, g.DiscordID)
	if err != nil {
		b.log.Warn("game players for the survey", "guild", g.DiscordID, "err", err)
		return nil
	}
	claimed, err := b.api.ClaimDispatch(ctx, g.DiscordID, "game_guess", week)
	if err != nil || !claimed {
		return nil
	}
	if r.Answers < r.MinAnswers {
		link = ""
	}
	return &guessInSurvey{week: week, round: r, link: link, players: players}
}

// plays reports whether the member plays the game.
func (q *guessInSurvey) plays(id string) bool {
	for _, p := range q.players {
		if p.DiscordID == id {
			return true
		}
	}
	return false
}

// gameEmbed is the survey's line about the game.
func gameEmbed(l *i18n.Locale, q *guessInSurvey) *discordgo.MessageEmbed {
	text := l.T("game.in_survey_propose")
	if q.link != "" {
		text = l.T("game.in_survey_guess", "count", q.round.Answers, "question", q.round.Question)
	}
	return &discordgo.MessageEmbed{Description: text, Color: surveyColor}
}

// gameRow is the survey's button about the game: guess on the web, or propose a question.
func gameRow(l *i18n.Locale, g api.Guild, q *guessInSurvey) discordgo.ActionsRow {
	btn := discordgo.Button{
		Label: l.T("game.propose_button"), Emoji: &discordgo.ComponentEmoji{Name: "💡"}, Style: discordgo.SecondaryButton,
		CustomID: gameID{Action: gamePropose, GuildID: g.DiscordID, Week: q.week}.String(),
	}
	if q.link != "" {
		btn = discordgo.Button{Label: l.T("game.guess_button"), Emoji: &discordgo.ComponentEmoji{Name: "🔎"}, Style: discordgo.LinkButton, URL: q.link}
	}
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}}
}

// withGame adds the game's line and button to a survey.
func withGame(l *i18n.Locale, g api.Guild, msg *discordgo.MessageSend, q *guessInSurvey) *discordgo.MessageSend {
	msg.Embeds = append(msg.Embeds, gameEmbed(l, q))
	msg.Components = append(msg.Components, gameRow(l, g, q))
	return msg
}

// gameCarried is the game's line and button on a survey card, to keep them when the card changes.
type gameCarried struct {
	embeds []*discordgo.MessageEmbed
	row    []discordgo.MessageComponent
}

// carriedGame reads them from m: the embeds after the survey's own, and the row with the game's
// button.
func carriedGame(m *discordgo.Message) gameCarried {
	var c gameCarried
	if m == nil {
		return c
	}
	if len(m.Embeds) > 1 {
		c.embeds = m.Embeds[1:]
	}
	for _, comp := range m.Components {
		row, ok := comp.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, rc := range row.Components {
			if btn, ok := rc.(*discordgo.Button); ok && isGameButton(btn) {
				c.row = append(c.row, btn)
			}
		}
	}
	return c
}

func isGameButton(btn *discordgo.Button) bool {
	if btn.Style == discordgo.LinkButton {
		return strings.Contains(btn.URL, "/#/game/")
	}
	_, ok := parseGameID(btn.CustomID)
	return ok
}

func (c gameCarried) empty() bool { return len(c.embeds) == 0 && len(c.row) == 0 }

// onto puts the game's line and button back on a card's new content.
func (c gameCarried) onto(data *discordgo.InteractionResponseData) *discordgo.InteractionResponseData {
	if c.empty() {
		return data
	}
	data.Embeds = append(data.Embeds, c.embeds...)
	if len(c.row) > 0 {
		data.Components = append(data.Components, discordgo.ActionsRow{Components: c.row})
	}
	return data
}

// sendGuessDMs sends the game's own guessing DM to the players who didn't get it with the survey,
// and records the game's dispatch with everyone it reached.
func (b *Bot) sendGuessDMs(ctx context.Context, g api.Guild, q *guessInSurvey, inSurvey map[string]bool, surveyFailed int) {
	var rest []api.Member
	for _, p := range q.players {
		if !inSurvey[p.DiscordID] {
			rest = append(rest, p)
		}
	}
	d := delivery{}
	if len(rest) > 0 {
		d, _ = b.dmAll(g, rest, gameGuessMessage(i18n.Get(g.Language), g, q.round, q.link), "game guess")
	}
	recipients, failed := len(q.players), d.failed+surveyFailed
	if recipients == 0 {
		return
	}
	if err := b.api.FinishDispatch(ctx, g.DiscordID, "game_guess", q.week, recipients, failed); err != nil {
		b.log.Warn("game guess dispatch counts", "guild", g.DiscordID, "err", err)
	}
}
