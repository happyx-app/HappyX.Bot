package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// fake answers both the HappyX API and Discord for one test: each handler gets the method and
// path, and what it doesn't know answers 404.
type fake struct {
	mu    sync.Mutex
	calls []string
	h     map[string]func(w http.ResponseWriter, r *http.Request)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	h, ok := f.h[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"not found","message":"Unknown","code":10008}`)
		return
	}
	h(w, r)
}

func (f *fake) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func reply(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }
}

// fakeBot is a Bot whose API and Discord are f, at clock.
func fakeBot(t *testing.T, f *fake, clock time.Time) *Bot {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	oldCh, oldUsers := discordgo.EndpointChannels, discordgo.EndpointUsers
	t.Cleanup(func() { discordgo.EndpointChannels, discordgo.EndpointUsers = oldCh, oldUsers })
	discordgo.EndpointChannels = srv.URL + "/discord/channels/"
	discordgo.EndpointUsers = srv.URL + "/discord/users/"
	dg, _ := discordgo.New("Bot x")
	return &Bot{api: api.New(srv.URL, "key"), dg: dg, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		now: func() time.Time { return clock }, webURL: "https://happyx.example.com"}
}

const (
	fGuild = "100000000000000001"
	fUser  = "200000000000000001"
	fDM    = "500000000000000001"
)

// «Your week» shows on the week's last survey day only, with a dash for a day without an answer.
func TestThanksWeek(t *testing.T) {
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"GET /v1/guilds/" + fGuild + "/members/" + fUser + "/moods": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("from") != "2026-10-19" || r.URL.Query().Get("to") != "2026-10-23" {
				t.Errorf("week asked: %s", r.URL.RawQuery)
			}
			io.WriteString(w, `[{"day":"2026-10-19","mood":3},{"day":"2026-10-20","mood":4},{"day":"2026-10-22","mood":5},{"day":"2026-10-23","mood":2}]`)
		},
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	es := i18n.Get("es")
	g := api.Guild{DiscordID: fGuild, Name: "Equipo", WorkingDays: presetDays("mon_fri")}
	user := &discordgo.User{ID: fUser, Username: "ana"}

	friday := surveyID{Action: actionMood, GuildID: fGuild, Day: "2026-10-23", Mood: 4}
	data := b.thanks(context.Background(), es, g, friday, user, "Semana intensa")
	d := data.Embeds[0].Description
	if !strings.HasSuffix(d, "\n\n**Tu semana:** lu 😐 · ma 🙂 · mi – · ju 🤩 · vi 🙂") {
		t.Fatalf("week line: %q", d) // today's answer is the one just given, 🙂
	}
	row := data.Components[0].(discordgo.ActionsRow).Components
	if last := row[len(row)-1].(discordgo.Button); last.URL != "https://happyx.example.com/#/me?g="+fGuild {
		t.Errorf("evolution link: %+v", row)
	}
	if q := answeredQuote(&discordgo.Message{Embeds: data.Embeds}); q != "> Semana intensa" {
		t.Errorf("the quote kept by «Change answer» takes the week along: %q", q)
	}

	thursday := surveyID{Action: actionMood, GuildID: fGuild, Day: "2026-10-22", Mood: 4}
	if d := b.thanks(context.Background(), es, g, thursday, user, "").Embeds[0].Description; strings.Contains(d, "Tu semana") {
		t.Errorf("only the last survey day shows the week: %q", d)
	}
}

// «Change answer» after midnight asks about yesterday.
func TestYesterdayQuestion(t *testing.T) {
	es := i18n.Get("es")
	g := api.Guild{DiscordID: fGuild, Name: "Equipo"}
	if d := surveyMessage(es, g, "2026-10-22", surveyRecipient{Name: "Ana", Yesterday: true}, "").Embeds[0].Description; !strings.Contains(d, "¿Cómo estuviste ayer?") {
		t.Errorf("yesterday's survey: %q", d)
	}
	if d := surveyMessage(es, g, "2026-10-22", surveyRecipient{Name: "Ana"}, "").Embeds[0].Description; !strings.Contains(d, "hoy") {
		t.Errorf("today's survey: %q", d)
	}
	b := &Bot{now: func() time.Time { return time.Date(2026, 10, 22, 23, 30, 0, 0, time.UTC) }} // 00:30 in Madrid
	if !b.yesterday(api.Guild{Timezone: "Europe/Madrid"}, "2026-10-22") {
		t.Error("in Madrid it's already the 23rd")
	}
}

// Resuming ends a vacation going on today, and says so; one planned for later stays.
func TestResumeVacation(t *testing.T) {
	vacations := `[{"from":"2026-10-20","to":"2026-10-30"}]`
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"PUT /v1/guilds/" + fGuild + "/members/" + fUser:                reply(`{}`),
		"GET /v1/guilds/" + fGuild + "/members/" + fUser + "/vacations": func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, vacations) },
		"DELETE /v1/guilds/" + fGuild + "/members/" + fUser + "/vacations": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	g := api.Guild{DiscordID: fGuild, Name: "Equipo", Timezone: "Europe/Madrid"}
	user := &discordgo.User{ID: fUser, Username: "ana"}
	ended, err := b.resume(context.Background(), g, user)
	if err != nil || ended == nil || !f.called("DELETE /v1/guilds/"+fGuild+"/members/"+fUser+"/vacations") {
		t.Fatalf("a vacation on today ends: %v %v", ended, err)
	}
	if text := resumedText(i18n.Get("es"), g.Name, ended); !strings.Contains(text, "terminan hoy, en todos tus servidores") {
		t.Errorf("says so: %q", text)
	}

	f.calls, vacations = nil, `[{"from":"2026-12-20","to":"2027-01-06"}]`
	if ended, err := b.resume(context.Background(), g, user); err != nil || ended != nil || f.called("DELETE /v1/guilds/"+fGuild+"/members/"+fUser+"/vacations") {
		t.Fatalf("a vacation planned for later stays: %v %v", ended, err)
	}
}

// Pausing or resuming from /happyx status changes the survey card in the DM too.
func TestSyncSurveyCard(t *testing.T) {
	es := i18n.Get("es")
	g := api.Guild{DiscordID: fGuild, Name: "Equipo", Timezone: "Europe/Madrid"}
	open := surveyMessage(es, g, "2026-10-23", surveyRecipient{Name: "Ana"}, "")
	card := func(components []discordgo.MessageComponent) string {
		m, _ := json.Marshal([]map[string]any{{"id": "600000000000000001", "channel_id": fDM, "components": components}})
		return string(m)
	}
	dm := card(open.Components)
	var edited map[string]any
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"POST /discord/users/@me/channels":           reply(`{"id":"` + fDM + `"}`),
		"GET /discord/channels/" + fDM + "/messages": func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, dm) },
		"PATCH /discord/channels/" + fDM + "/messages/600000000000000001": func(w http.ResponseWriter, r *http.Request) {
			edited = map[string]any{}
			json.NewDecoder(r.Body).Decode(&edited)
			io.WriteString(w, `{"id":"600000000000000001"}`)
		},
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	user := &discordgo.User{ID: fUser}
	if !b.syncSurveyCard(es, g, user, true) || !strings.Contains(asJSON(edited), "Pausada") {
		t.Fatalf("pausing turns the open survey into the paused card: %v", edited)
	}
	if b.syncSurveyCard(es, g, user, false) {
		t.Error("resuming leaves an open survey as it is")
	}

	dm, edited = card(*pausedEdit(es, g, "2026-10-23", fDM, "1").Components), nil
	if !b.syncSurveyCard(es, g, user, false) || !strings.Contains(asJSON(edited), "hx:mood:") {
		t.Fatalf("resuming brings today's survey back: %v", edited)
	}

	answered := answeredMessage(es, g, surveyID{Action: actionMood, GuildID: fGuild, Day: "2026-10-23", Mood: 4}, "Ana", "")
	dm, edited = card(answered.Components), nil
	if b.syncSurveyCard(es, g, user, true) || edited != nil {
		t.Error("an answered survey stays as it is")
	}
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// The game's line rides on Thursday's survey and stays through the answer and the close.
func TestGameInSurvey(t *testing.T) {
	es := i18n.Get("es")
	g := api.Guild{DiscordID: fGuild, Name: "Equipo"}
	q := &guessInSurvey{week: "2026-10-19", round: api.GameRound{Week: "2026-10-19", Question: "¿Tu primer trabajo?", Answers: 7}, link: "https://happyx.example.com/#/game/" + fGuild}
	msg := withGame(es, g, surveyMessage(es, g, "2026-10-22", surveyRecipient{Name: "Ana"}, ""), q)
	if len(msg.Embeds) != 2 || !strings.Contains(msg.Embeds[1].Description, "7 compañeros han respondido a «¿Tu primer trabajo?»") || len(msg.Components) != 3 {
		t.Fatalf("survey with the game: %+v", msg)
	}
	// As Discord sends the card back: rows and buttons as pointers.
	var sent discordgo.Message
	raw, _ := json.Marshal(map[string]any{"embeds": msg.Embeds, "components": msg.Components})
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	c := carriedGame(&sent)
	if len(c.embeds) != 1 || len(c.row) != 1 || c.row[0].(*discordgo.Button).URL != q.link {
		t.Fatalf("carried: %+v", c)
	}
	thanks := c.onto(answeredMessage(es, g, surveyID{Action: actionMood, GuildID: fGuild, Day: "2026-10-22", Mood: 4}, "Ana", ""))
	if len(thanks.Embeds) != 2 || len(thanks.Components) != 2 {
		t.Errorf("the thanks card keeps the game: %+v", thanks)
	}
	if c := carriedGame(&discordgo.Message{Embeds: msg.Embeds[:1]}); !c.empty() {
		t.Errorf("a survey without the game carries nothing: %+v", c)
	}

	q.link = ""
	propose := withGame(es, g, surveyMessage(es, g, "2026-10-22", surveyRecipient{Name: "Ana"}, ""), q)
	btn := propose.Components[2].(discordgo.ActionsRow).Components[0].(discordgo.Button)
	if id, ok := parseGameID(btn.CustomID); !ok || id.Action != gamePropose || !strings.Contains(propose.Embeds[1].Description, "Propón") {
		t.Errorf("too few answers: propose instead: %+v", propose.Embeds[1])
	}
}

// The survey's channel message is posted once someone's DMs are closed, and not again while it's there.
func TestEnsureSurveyPost(t *testing.T) {
	post := `{"kind":"survey","channel_id":"400000000000000001","message_id":"700000000000000001"}`
	posted, alive := "", true
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"GET /v1/guilds/" + fGuild + "/channel-posts/survey": func(w http.ResponseWriter, _ *http.Request) {
			if post == "" {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"error":"not found"}`)
				return
			}
			io.WriteString(w, post)
		},
		"GET /discord/channels/400000000000000001/messages/700000000000000001": func(w http.ResponseWriter, _ *http.Request) {
			if !alive {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"message":"Unknown Message","code":10008}`)
				return
			}
			io.WriteString(w, `{"id":"700000000000000001"}`)
		},
		"POST /discord/channels/400000000000000001/messages": func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			posted = string(raw)
			io.WriteString(w, `{"id":"700000000000000002","channel_id":"400000000000000001"}`)
		},
		"PUT /v1/guilds/" + fGuild + "/channel-posts/survey": reply(`{}`),
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	g := api.Guild{DiscordID: fGuild, Name: "Equipo", Language: "es", KudosChannelID: "400000000000000001"}

	b.ensureSurveyPost(context.Background(), g)
	if posted != "" {
		t.Fatal("a message still there isn't posted again")
	}
	alive = false
	b.ensureSurveyPost(context.Background(), g)
	if !strings.Contains(posted, "Responder hoy") || !strings.Contains(posted, "hx:answer:"+fGuild) || !f.called("PUT /v1/guilds/"+fGuild+"/channel-posts/survey") {
		t.Fatalf("a deleted message is posted again and recorded: %s", posted)
	}
	posted, post = "", ""
	b.ensureSurveyPost(context.Background(), api.Guild{DiscordID: fGuild, Name: "Equipo"})
	if posted != "" {
		t.Error("without a team channel nothing is posted")
	}
}

func TestClosedDMs(t *testing.T) {
	closed := &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: discordgo.ErrCodeCannotSendMessagesToThisUser}}
	if !closedDMs(closed) || closedDMs(errors.New("timeout")) || closedDMs(&discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: 50013}}) {
		t.Error("only 50007 means DMs closed")
	}
	if id, ok := parseSurveyID(surveyID{Action: actionAnswer, GuildID: fGuild}.String()); !ok || id.Action != actionAnswer {
		t.Error("«Answer today» parses")
	}
}

// The open survey from the channel is today's once it went out, otherwise yesterday's.
func TestOpenSurveyDay(t *testing.T) {
	g := api.Guild{Timezone: "Europe/Madrid", SurveyHour: 13, WorkingDays: presetDays("mon_fri")}
	at := func(s string) *Bot {
		tm, _ := time.Parse(time.RFC3339, s)
		return &Bot{now: func() time.Time { return tm }}
	}
	cases := map[string]string{
		"2026-10-22T12:00:00Z": "2026-10-22", // Thursday 14:00
		"2026-10-22T08:00:00Z": "2026-10-21", // Thursday 10:00: Wednesday's still open
		"2026-10-26T08:00:00Z": "",           // Monday 09:00: Sunday had none
	}
	for now, want := range cases {
		day, ok := at(now).openSurveyDay(g)
		if (want == "") == ok || day != want {
			t.Errorf("%s: %q %v, want %q", now, day, ok, want)
		}
	}
}

// /happyx in a DM asks which server when there are several.
func TestServerPicker(t *testing.T) {
	es := i18n.Get("es")
	p := serverPicker(es, cmdPause, []api.Guild{{DiscordID: "1", Name: "A"}, {DiscordID: "2", Name: "B"}})
	menu := p.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if id, ok := parseDMID(menu.CustomID); !ok || id.Command != cmdPause || len(menu.Options) != 2 || p.Content != "¿Qué encuesta quieres pausar?" {
		t.Fatalf("picker: %+v", p)
	}
	if _, ok := parseDMID("hxd:config"); ok {
		t.Error("only status, pause and resume pick a server")
	}
}

// On Thursday the game's guess rides on the survey of those who play; whoever plays without the
// survey gets the game's own DM, and a closed DM puts the survey in the team channel.
func TestSurveyCarriesGuess(t *testing.T) {
	const ana, bea, carl = "200000000000000001", "200000000000000002", "200000000000000003"
	var mu sync.Mutex
	sent := map[string]string{} // DM channel (the user's ID here) → body
	var finished string
	channelPosted := false
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"GET /v1/guilds/" + fGuild + "/members": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("for") == "game" {
				io.WriteString(w, `[{"discord_user_id":"`+ana+`","username":"ana"},{"discord_user_id":"`+carl+`","username":"carl"}]`)
				return
			}
			io.WriteString(w, `[{"discord_user_id":"`+ana+`","username":"ana"},{"discord_user_id":"`+bea+`","username":"bea"}]`)
		},
		"GET /v1/guilds/" + fGuild + "/game/rounds/2026-10-19": reply(`{"week":"2026-10-19","question":"¿Tu primer trabajo?","answers":7,"min_answers":3}`),
		"POST /v1/guilds/" + fGuild + "/dispatches":            reply(`{}`),
		"PUT /v1/guilds/" + fGuild + "/dispatches/game_guess/2026-10-19": func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			finished = string(raw)
			w.WriteHeader(http.StatusNoContent)
		},
		"POST /discord/users/@me/channels": func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Recipient string `json:"recipient_id"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			io.WriteString(w, `{"id":"`+in.Recipient+`"}`)
		},
		"GET /v1/guilds/" + fGuild + "/channel-posts/survey": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"not found"}`)
		},
		"PUT /v1/guilds/" + fGuild + "/channel-posts/survey": reply(`{}`),
	}}
	for _, id := range []string{ana, bea, carl} {
		id := id
		f.h["POST /discord/channels/"+id+"/messages"] = func(w http.ResponseWriter, r *http.Request) {
			if id == bea {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"message":"Cannot send messages to this user","code":50007}`)
				return
			}
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			sent[id] = string(raw)
			mu.Unlock()
			io.WriteString(w, `{"id":"600000000000000009","channel_id":"`+id+`"}`)
		}
		f.h["GET /discord/channels/"+id+"/messages"] = reply(`[]`)
	}
	f.h["POST /discord/channels/400000000000000001/messages"] = func(w http.ResponseWriter, _ *http.Request) {
		channelPosted = true
		io.WriteString(w, `{"id":"700000000000000001","channel_id":"400000000000000001"}`)
	}
	b := fakeBot(t, f, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) // Thursday, 14:00 in Madrid
	g := api.Guild{DiscordID: fGuild, Name: "Equipo", Language: "es", Timezone: "Europe/Madrid", SurveyHour: 13,
		WorkingDays: presetDays("mon_fri"), GamesEnabled: true, KudosChannelID: "400000000000000001"}

	d, err := b.sendSurvey(context.Background(), g, "2026-10-22")
	if err != nil || d.recipients != 2 || d.failed != 1 || d.closed != 1 {
		t.Fatalf("survey delivery: %+v %v", d, err)
	}
	if !strings.Contains(sent[ana], "7 compañeros han respondido") {
		t.Errorf("Ana plays: her survey carries the game: %s", sent[ana])
	}
	if !strings.Contains(sent[carl], "¿Adivinas quién escribió") || strings.Contains(sent[carl], "hx:mood") {
		t.Errorf("Carl plays without the survey: the game's own DM: %s", sent[carl])
	}
	if !strings.Contains(finished, `"recipients":2`) || !strings.Contains(finished, `"failed":0`) {
		t.Errorf("game dispatch counts: %s", finished)
	}
	if !channelPosted {
		t.Error("Bea's DMs are closed: the survey goes to the team channel")
	}
}
