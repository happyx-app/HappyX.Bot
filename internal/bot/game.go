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

// «Who's who?»: on Monday every member gets the week's question by DM and answers it in a
// modal (or on the web) until Wednesday. On Thursday they get a link to guess on the web who
// wrote what, and a button to propose next week's question. Rounds, answers, proposals and
// phases live in the API; the bot only offers a question, which a proposal replaces.

const (
	gameIDPrefix    = "hxg"
	gameAnswer      = "answer"      // button: open the answer modal
	gameAnswerModal = "answermodal" // modal submit: record the answer
	gameAnswerMax   = 200           // the API's limit

	gamePropose      = "propose"      // button: open the proposal modal
	gameProposeModal = "proposemodal" // modal submit: record the proposal
	gameProposeMax   = 200            // the API's limit
)

// Game components carry the guild and the round's Monday, like the survey's.
type gameID struct {
	Action  string
	GuildID string
	Week    string
}

func (g gameID) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", gameIDPrefix, g.Action, g.GuildID, g.Week)
}

func parseGameID(v string) (gameID, bool) {
	p := strings.Split(v, ":")
	if len(p) != 4 || p[0] != gameIDPrefix || (p[1] != gameAnswer && p[1] != gameAnswerModal && p[1] != gamePropose && p[1] != gameProposeModal) {
		return gameID{}, false
	}
	if _, err := time.Parse(schedule.DateLayout, p[3]); err != nil {
		return gameID{}, false
	}
	return gameID{Action: p[1], GuildID: p[2], Week: p[3]}, true
}

// gameQuestion picks the week's question. They take turns week by week, so a guild goes
// through them all before any repeats.
func gameQuestion(l *i18n.Locale, week string) string {
	en := i18n.Get(i18n.Default)
	n := 0
	for en.Has(fmt.Sprintf("game.q.%d", n+1)) {
		n++
	}
	d, _ := time.Parse(schedule.DateLayout, week)
	weeks := int(d.Unix() / (7 * 24 * 60 * 60))
	return l.T(fmt.Sprintf("game.q.%d", weeks%n+1))
}

// gameURL is the guild's game page on the dashboard, or "" when the bot doesn't know the
// dashboard's address.
func (b *Bot) gameURL(guildID string) string {
	if b.webURL == "" {
		return ""
	}
	return b.webURL + "/#/game/" + guildID
}

func (b *Bot) sendGameQuestion(ctx context.Context, g api.Guild, week string) (delivery, error) {
	l := i18n.Get(g.Language)
	r, err := b.api.StartGameRound(ctx, g.DiscordID, week, gameQuestion(l, week))
	if errors.Is(err, api.ErrConflict) {
		// Games were turned off, or answering closed, since the guild list was read.
		b.log.Info("game round not started", "guild", g.DiscordID, "week", week, "err", err)
		return delivery{}, nil
	}
	if err != nil {
		return delivery{}, err
	}
	members, err := b.api.GamePlayers(ctx, g.DiscordID)
	if err != nil {
		return delivery{}, err
	}
	msg := gameQuestionMessage(l, g, r, b.gameURL(g.DiscordID))
	return b.dmAll(g, members, msg, "game question")
}

// sendGameGuess tells every member guessing is open, if the round got enough answers and there
// is a dashboard to guess on, and asks them for next week's question either way.
func (b *Bot) sendGameGuess(ctx context.Context, g api.Guild, week string) (delivery, error) {
	link := b.gameURL(g.DiscordID)
	if link == "" {
		b.log.Warn("WEB_URL not set, the game's Thursday DM only asks for next week's question", "guild", g.DiscordID)
	}
	r, err := b.api.GameRound(ctx, g.DiscordID, week)
	if errors.Is(err, api.ErrNotFound) {
		return delivery{}, nil // no round this week
	}
	if err != nil {
		return delivery{}, err
	}
	if r.Answers < r.MinAnswers {
		b.log.Info("game round has too few answers to guess", "guild", g.DiscordID, "week", week, "answers", r.Answers)
		link = ""
	}
	members, err := b.api.GamePlayers(ctx, g.DiscordID)
	if err != nil {
		return delivery{}, err
	}
	return b.dmAll(g, members, gameGuessMessage(i18n.Get(g.Language), g, r, link), "game guess")
}

func (b *Bot) dmAll(g api.Guild, members []api.Member, msg *discordgo.MessageSend, what string) (delivery, error) {
	return b.dmEach(g, members, func(api.Member) *discordgo.MessageSend { return msg }, nil, what)
}

// dmEach is dmAll with a message built for each member: it sends msg to each member by DM and,
// when after is set, calls it with each DM sent.
func (b *Bot) dmEach(g api.Guild, members []api.Member, msg func(api.Member) *discordgo.MessageSend, after func(m api.Member, sent *discordgo.Message), what string) (delivery, error) {
	d := delivery{recipients: len(members)}
	for _, m := range members {
		sent, err := b.dmMessage(m.DiscordID, msg(m))
		if err != nil {
			// Usually the user has DMs from server members turned off.
			b.log.Warn(what+" DM failed", "guild", g.DiscordID, "user", m.DiscordID, "err", err)
			d.failed++
			if closedDMs(err) {
				d.closed++
			}
			continue
		}
		if after != nil {
			after(m, sent)
		}
	}
	if d.failed > 0 && d.failed == len(members) {
		return d, fmt.Errorf("could not DM any of %d members", d.failed)
	}
	return d, nil
}

func gameQuestionMessage(l *i18n.Locale, g api.Guild, r api.GameRound, link string) *discordgo.MessageSend {
	content := gameHeader(l, g, r.Question) + "\n"
	if r.Proposed {
		content += l.T("game.proposed") + "\n"
	}
	return &discordgo.MessageSend{
		Content: content + l.T("game.howto"),
		Components: gameButtons(l, link, discordgo.Button{
			Label: l.T("game.answer_button"), Emoji: &discordgo.ComponentEmoji{Name: "✏️"}, Style: discordgo.PrimaryButton,
			CustomID: gameID{Action: gameAnswer, GuildID: g.DiscordID, Week: r.Week}.String(),
		}),
	}
}

// gameAnsweredMessage replaces the question once answered; the answer can still change.
func gameAnsweredMessage(l *i18n.Locale, g api.Guild, question, week, answer, link string) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		Content: gameHeader(l, g, question) + "\n" + quote(answer) + "\n" + l.T("game.answered"),
		Components: gameButtons(l, link, discordgo.Button{
			Label: l.T("game.change_button"), Emoji: &discordgo.ComponentEmoji{Name: "✏️"}, Style: discordgo.SecondaryButton,
			CustomID: gameID{Action: gameAnswer, GuildID: g.DiscordID, Week: week}.String(),
		}),
	}
}

// gameGuessMessage opens guessing with a link to the web, or with no link (too few answers, or
// no dashboard) only asks for next week's question.
func gameGuessMessage(l *i18n.Locale, g api.Guild, r api.GameRound, link string) *discordgo.MessageSend {
	content := l.T("game.title", "guild", g.Name) + "\n"
	var row []discordgo.MessageComponent
	if link != "" {
		content += l.T("game.guess", "count", r.Answers, "question", r.Question) + "\n"
		row = append(row, discordgo.Button{Label: l.T("game.guess_button"), Emoji: &discordgo.ComponentEmoji{Name: "🔎"}, Style: discordgo.LinkButton, URL: link})
	}
	row = append(row, discordgo.Button{
		Label: l.T("game.propose_button"), Emoji: &discordgo.ComponentEmoji{Name: "💡"}, Style: discordgo.SecondaryButton,
		CustomID: gameID{Action: gamePropose, GuildID: g.DiscordID, Week: r.Week}.String(),
	})
	return &discordgo.MessageSend{
		Content:    content + l.T("game.propose"),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}},
	}
}

func gameHeader(l *i18n.Locale, g api.Guild, question string) string {
	return l.T("game.title", "guild", g.Name) + "\n" + l.T("game.question", "question", question)
}

// gameButtons is one row with button and, when there is a dashboard, a link to it.
func gameButtons(l *i18n.Locale, link string, button discordgo.Button) []discordgo.MessageComponent {
	row := []discordgo.MessageComponent{button}
	if link != "" {
		row = append(row, discordgo.Button{Label: l.T("game.web_button"), Emoji: &discordgo.ComponentEmoji{Name: "🌐"}, Style: discordgo.LinkButton, URL: link})
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}}
}

// gameModal is the answer modal for a gameAnswer button and the proposal one for gamePropose.
func gameModal(l *i18n.Locale, id gameID) *discordgo.InteractionResponseData {
	input := discordgo.TextInput{
		CustomID:    "answer",
		Label:       l.T("game.modal_label"),
		Style:       discordgo.TextInputParagraph,
		Required:    true,
		MaxLength:   gameAnswerMax,
		Placeholder: l.T("game.modal_placeholder"),
	}
	title := l.T("game.modal_title")
	if id.Action == gamePropose {
		id.Action = gameProposeModal
		title = l.T("game.propose_modal_title")
		input.CustomID, input.Label, input.MaxLength, input.Placeholder =
			"question", l.T("game.propose_modal_label"), gameProposeMax, l.T("game.propose_modal_placeholder")
	} else {
		id.Action = gameAnswerModal
	}
	return &discordgo.InteractionResponseData{
		CustomID:   id.String(),
		Title:      title,
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{input}}},
	}
}

func (b *Bot) handleGameButton(ctx context.Context, i *discordgo.InteractionCreate, id gameID) {
	b.respond(i, discordgo.InteractionResponseModal, gameModal(b.guildLocale(ctx, id.GuildID), id))
}

// handleProposeModal records the member's proposal and confirms it privately, leaving the
// Thursday DM as it was so the guess link stays.
func (b *Bot) handleProposeModal(ctx context.Context, i *discordgo.InteractionCreate, id gameID, data discordgo.ModalSubmitInteractionData) {
	l := b.guildLocale(ctx, id.GuildID)
	question := strings.TrimSpace(modalValue(data, "question"))
	if err := b.api.ProposeQuestion(ctx, id.GuildID, interactionUser(i).ID, question); err != nil {
		var apiErr *api.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Message == api.MsgGameClosed:
			b.ephemeral(i, l.T("game.propose_closed"))
		case errors.As(err, &apiErr) && apiErr.Message == api.MsgGamesOff:
			b.ephemeral(i, l.T("game.off"))
		default:
			b.replyError(i, l, err)
		}
		return
	}
	b.ephemeral(i, l.T("game.proposed_ok", "question", question))
}

func (b *Bot) handleGameModal(ctx context.Context, i *discordgo.InteractionCreate, id gameID, data discordgo.ModalSubmitInteractionData) {
	g, err := b.api.Guild(ctx, id.GuildID)
	l := i18n.Get(g.Language)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	answer := strings.TrimSpace(modalValue(data, "answer"))
	if err := b.api.AnswerGame(ctx, id.GuildID, id.Week, interactionUser(i).ID, answer); err != nil {
		var apiErr *api.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Message == api.MsgGameClosed:
			b.ephemeral(i, l.T("game.closed"))
		case errors.As(err, &apiErr) && apiErr.Message == api.MsgGamesOff:
			b.ephemeral(i, l.T("game.off"))
		default:
			b.replyError(i, l, err)
		}
		return
	}
	question := ""
	if r, err := b.api.GameRound(ctx, id.GuildID, id.Week); err == nil {
		question = r.Question
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, gameAnsweredMessage(l, g, question, id.Week, answer, b.gameURL(g.DiscordID)))
}

// gameLine says whether the game is on, for /happyx status and config.
func gameLine(l *i18n.Locale, g api.Guild) string {
	return l.T("game.line", "state", yesNo(l, g.GamesEnabled))
}
