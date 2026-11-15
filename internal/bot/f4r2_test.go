package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// answerFake is the API and Discord for «Answer today» pressed in the team channel. It records
// the day asked for the survey's recipients and the card shown.
func answerFake(t *testing.T, clock time.Time, answered bool) (*Bot, *string, map[string]any) {
	t.Helper()
	day, card := new(string), map[string]any{}
	moods := `[]`
	if answered {
		moods = `[{"day":"2026-10-22","mood":4}]`
	}
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"GET /v1/guilds/" + fGuild: reply(`{"discord_guild_id":"` + fGuild + `","name":"Equipo","timezone":"Europe/Madrid","survey_hour":13,"language":"es","games_enabled":true,"working_days":[1,2,3,4,5]}`),
		"GET /v1/guilds/" + fGuild + "/members": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("for") == "survey" {
				*day = r.URL.Query().Get("day")
			}
			io.WriteString(w, `[{"discord_user_id":"`+fUser+`","username":"ana","active":true}]`)
		},
		"GET /v1/guilds/" + fGuild + "/members/" + fUser + "/moods": reply(moods),
		"GET /v1/guilds/" + fGuild + "/game/rounds/2026-10-19":      reply(`{"week":"2026-10-19","question":"¿Playa o montaña?","phase":"guessing","answers":5,"min_answers":3}`),
		"POST /interactions/1/tok/callback":                         reply(`{}`),
		"PATCH /webhooks/app/tok/messages/@original": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&card)
			io.WriteString(w, `{"id":"1"}`)
		},
	}}
	b := fakeBot(t, f, clock)
	oldResp, oldEdit := discordgo.EndpointInteractionResponse, discordgo.EndpointWebhookMessage
	t.Cleanup(func() { discordgo.EndpointInteractionResponse, discordgo.EndpointWebhookMessage = oldResp, oldEdit })
	base := strings.TrimSuffix(discordgo.EndpointChannels, "/discord/channels/")
	discordgo.EndpointInteractionResponse = func(id, token string) string { return base + "/interactions/" + id + "/" + token + "/callback" }
	discordgo.EndpointWebhookMessage = func(id, token, msg string) string { return base + "/webhooks/" + id + "/" + token + "/messages/" + msg }
	return b, day, card
}

func pressAnswer(b *Bot) {
	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID: "1", AppID: "app", Token: "tok", GuildID: fGuild, Type: discordgo.InteractionMessageComponent,
		Member: &discordgo.Member{User: &discordgo.User{ID: fUser, Username: "ana"}},
	}}
	b.answerFromChannel(context.Background(), i, surveyID{Action: actionAnswer, GuildID: fGuild})
}

// cardText is everything the card says, buttons included.
func cardText(c map[string]any) string {
	b, _ := json.Marshal(c)
	return string(b)
}

// On Thursday the card from the channel carries the game, as the DM's survey does.
func TestAnswerFromChannelGame(t *testing.T) {
	b, day, card := answerFake(t, time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC), false) // Thursday, 16:00 in Madrid
	pressAnswer(b)
	if *day != "2026-10-22" {
		t.Errorf("recipients of %q, want today's", *day)
	}
	got := cardText(card)
	if !strings.Contains(got, "Adivina quién escribió cada respuesta") || !strings.Contains(got, "/#/game/") {
		t.Errorf("no game on Thursday's card: %s", got)
	}

	// Already answered: the thanks card keeps the game too.
	b, _, card = answerFake(t, time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC), true)
	pressAnswer(b)
	if got := cardText(card); !strings.Contains(got, "Apuntado") || !strings.Contains(got, "/#/game/") {
		t.Errorf("thanks card without the game: %s", got)
	}
}

// Before today's survey goes out, the open one is yesterday's: who could answer it is who got it.
func TestAnswerFromChannelYesterday(t *testing.T) {
	b, day, card := answerFake(t, time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC), false) // Friday, 10:00 in Madrid
	pressAnswer(b)
	if *day != "2026-10-22" {
		t.Errorf("recipients of %q, want yesterday's", *day)
	}
	if got := cardText(card); !strings.Contains(got, "¿Cómo estuviste ayer?") {
		t.Errorf("not yesterday's survey: %s", got)
	}
}

// /happyx status tells admins when people with DMs closed have nowhere to answer.
func TestClosedDMsWarning(t *testing.T) {
	es := i18n.Get("es")
	b := &Bot{}
	g := api.Guild{DiscordID: fGuild, Name: "Equipo"}
	if got := b.closedDMsWarning(es, g, 2); !strings.Contains(got, "2 personas tienen cerrados los mensajes privados") || !strings.Contains(got, "/happyx config") {
		t.Errorf("warning: %q", got)
	}
	if got := b.closedDMsWarning(es, g, 1); !strings.Contains(got, "1 persona tiene cerrados") {
		t.Errorf("one person: %q", got)
	}
	if got := b.closedDMsWarning(es, g, 0); got != "" {
		t.Errorf("nobody closed: %q", got)
	}
	g.KudosChannelID = "400000000000000001"
	if got := b.closedDMsWarning(es, g, 2); got != "" {
		t.Errorf("with a team channel they answer there: %q", got)
	}
}

// /happyx pause in a DM only offers the surveys the member still gets: with one left, it pauses
// that one without asking.
func TestDMPauseSkipsPaused(t *testing.T) {
	const other = "100000000000000002"
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"GET /v1/users/" + fUser + "/guilds":                reply(`[{"discord_guild_id":"` + fGuild + `","name":"Equipo","paused":false},{"discord_guild_id":"` + other + `","name":"Club","paused":true}]`),
		"DELETE /v1/guilds/" + fGuild + "/members/" + fUser: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		"POST /interactions/1/tok/callback":                 reply(`{}`),
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC))
	oldResp := discordgo.EndpointInteractionResponse
	t.Cleanup(func() { discordgo.EndpointInteractionResponse = oldResp })
	base := strings.TrimSuffix(discordgo.EndpointChannels, "/discord/channels/")
	discordgo.EndpointInteractionResponse = func(id, token string) string { return base + "/interactions/" + id + "/" + token + "/callback" }
	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID: "1", AppID: "app", Token: "tok", Type: discordgo.InteractionApplicationCommand, Locale: discordgo.SpanishES,
		User: &discordgo.User{ID: fUser, Username: "ana"},
	}}
	b.handleDMCommand(context.Background(), i, &discordgo.ApplicationCommandInteractionDataOption{Name: cmdPause})
	if !f.called("DELETE /v1/guilds/"+fGuild+"/members/"+fUser) || f.called("DELETE /v1/guilds/"+other+"/members/"+fUser) {
		t.Errorf("paused the wrong one: %v", f.calls)
	}
}
