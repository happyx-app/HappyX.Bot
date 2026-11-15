package bot

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

func TestTeamIDRoundTrip(t *testing.T) {
	for _, id := range []teamID{
		{Action: teamTell, GuildID: "100000000000000001", From: "2026-11-09", To: "2026-11-15"},
		{Action: teamTellModal, GuildID: "100000000000000001", From: "2026-10-01", To: "2026-10-31"},
		{Action: teamEdit, GuildID: "100000000000000001", Note: 7},
		{Action: teamEditModal, GuildID: "100000000000000001", Note: 7},
		{Action: teamDelete, GuildID: "100000000000000001", Note: 12},
		{Action: teamDeleteOK, GuildID: "100000000000000001", Note: 12},
		{Action: teamBack, GuildID: "100000000000000001", Note: 12},
	} {
		got, ok := parseTeamID(id.String())
		if !ok || got != id {
			t.Errorf("%q parsed as %+v, %v", id.String(), got, ok)
		}
		if len(id.String()) > 100 {
			t.Errorf("custom ID %q is over Discord's 100", id.String())
		}
	}
	for _, bad := range []string{
		"hxt:tell:100000000000000001:2026-11-09",            // no end
		"hxt:tell:100000000000000001:2026-11-09:nope",       // not a date
		"hxt:edit:100000000000000001:0",                     // no note
		"hxt:edit:100000000000000001:x",                     // not a number
		"hxt:what:100000000000000001:1",                     // unknown action
		"hxt:tell:abc:2026-11-09:2026-11-15",                // not a guild
		"hxs:tell:100000000000000001:2026-11-09:2026-11-15", // other prefix
	} {
		if _, ok := parseTeamID(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}

// The modal's title says where the note goes and fits Discord's limits in every language.
func TestTeamModalFits(t *testing.T) {
	for _, lang := range i18n.Codes() {
		l := i18n.Get(lang)
		for _, id := range []teamID{
			{Action: teamTellModal, GuildID: "100000000000000001", From: "2026-11-09", To: "2026-11-15"},
			{Action: teamEditModal, GuildID: "100000000000000001", Note: 1},
		} {
			for _, ch := range []string{"", "equipo", strings.Repeat("canal-muy-largo-", 6)} {
				m := teamModal(l, id, ch, "")
				if n := len([]rune(m.Title)); n == 0 || n > 45 {
					t.Errorf("%s: title %q is %d characters", lang, m.Title, n)
				}
				if ch == "equipo" && !strings.Contains(m.Title, "#equipo") {
					t.Errorf("%s: title %q should name the channel", lang, m.Title)
				}
				in := m.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.TextInput)
				if n := len([]rune(in.Label)); n > 45 {
					t.Errorf("%s: label %q is %d characters", lang, in.Label, n)
				}
				if n := len([]rune(in.Placeholder)); n > 100 {
					t.Errorf("%s: placeholder %q is %d characters", lang, in.Placeholder, n)
				}
			}
		}
	}
	m := teamModal(i18n.Get("es"), teamID{Action: teamEditModal, GuildID: "100000000000000001", Note: 1}, "equipo", "Martes sin reuniones")
	if in := m.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.TextInput); in.Value != "Martes sin reuniones" {
		t.Errorf("editing should start from the published text, got %q", in.Value)
	}
}

func reportButtons(msg *discordgo.MessageSend) []discordgo.Button {
	var out []discordgo.Button
	for _, c := range msg.Components {
		for _, b := range c.(discordgo.ActionsRow).Components {
			out = append(out, b.(discordgo.Button))
		}
	}
	return out
}

func TestReportButtons(t *testing.T) {
	es := i18n.Get("es")
	g := api.Guild{DiscordID: "100000000000000001", Name: "Test", KudosChannelID: "200000000000000001"}
	r := api.Report{From: "2026-11-09", To: "2026-11-15", Responses: 4, Respondents: 4, Average: 3,
		Distribution: map[string]int{"3": 4}, Days: []api.ReportDay{{Day: "2026-11-09", Responses: 4, Average: 3}}}
	o := reportOptions{Tell: true, DashboardURL: "https://happyx.example/#/g/100000000000000001?p=last-week"}

	bs := reportButtons(reportMessageWith(es, g, r, o))
	if len(bs) != 2 || bs[0].Label != "Contar al equipo" || bs[1].URL != o.DashboardURL {
		t.Fatalf("report should offer «Contar al equipo» and the dashboard: %+v", bs)
	}
	if id, ok := parseTeamID(bs[0].CustomID); !ok || id.Action != teamTell || id.From != r.From || id.To != r.To {
		t.Fatalf("«Contar al equipo» should carry the period: %q", bs[0].CustomID)
	}

	noChannel := g
	noChannel.KudosChannelID = ""
	if bs := reportButtons(reportMessageWith(es, noChannel, r, o)); len(bs) != 1 || bs[0].URL == "" {
		t.Errorf("without a channel, only the dashboard: %+v", bs)
	}
	withheld := r
	withheld.Withheld = true
	if bs := reportButtons(reportMessageWith(es, g, withheld, o)); len(bs) != 1 || bs[0].URL == "" {
		t.Errorf("a withheld report has nothing to tell: %+v", bs)
	}
	if bs := reportButtons(reportMessageWith(es, g, r, reportOptions{})); len(bs) != 0 {
		t.Errorf("without options, no buttons: %+v", bs)
	}
}

func TestSummaryLines(t *testing.T) {
	es := i18n.Get("es")
	r := api.Report{From: "2026-11-09", To: "2026-11-15", Responses: 41, Respondents: 9, Audience: 12, Average: 3.44,
		Distribution: map[string]int{"1": 1, "2": 3, "3": 10, "4": 20, "5": 7}}
	got := summary(es, r, reportOptions{Previous: &api.Report{Responses: 30, Average: 3.71, Distribution: map[string]int{"4": 30}}})
	for _, want := range []string{
		"**Ánimo medio:** 3,4 / 5",
		"−0,3 respecto a la semana anterior",
		"**Han respondido:** 9 de 12 personas",
		"**Mal o Muy mal:** 4 de 41 respuestas",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary should contain %q:\n%s", want, got)
		}
	}

	monthly := summary(es, r, reportOptions{Monthly: true, Previous: &api.Report{Responses: 30, Average: 3.31, Distribution: map[string]int{"3": 30}}})
	if !strings.Contains(monthly, "+0,1 respecto al mes anterior") {
		t.Errorf("monthly comparison:\n%s", monthly)
	}
	same := summary(es, r, reportOptions{Previous: &api.Report{Responses: 30, Average: 3.41, Distribution: map[string]int{"3": 30}}})
	if !strings.Contains(same, "igual que la semana anterior") {
		t.Errorf("rounded to the same value, the change is none:\n%s", same)
	}
	for _, p := range []*api.Report{nil, {Withheld: true, Responses: 3}, {Responses: 0}} {
		if s := summary(es, r, reportOptions{Previous: p}); strings.Contains(s, "anterior") {
			t.Errorf("no comparison without a previous average (%+v):\n%s", p, s)
		}
	}

	left := r
	left.Audience = 8 // people left since: the count would read 9 of 8
	if s := summary(es, left, reportOptions{}); !strings.Contains(s, "**Han respondido:** 9 personas") {
		t.Errorf("audience below respondents should not be shown:\n%s", s)
	}
	one := api.Report{Responses: 1, Respondents: 1, Average: 4, Distribution: map[string]int{"4": 1}}
	if s := summary(es, one, reportOptions{}); !strings.Contains(s, "**Ha respondido:** 1 persona") {
		t.Errorf("one person:\n%s", s)
	}
}

func TestTeamSummary(t *testing.T) {
	es := i18n.Get("es")
	g := api.Guild{DiscordID: "100000000000000001", Name: "Equipo", AnonymityThreshold: 3}
	round := &api.GameRound{Week: "2026-11-09", Question: "¿Tu primer trabajo?", Phase: "finished", Answers: 6, MinAnswers: 4}
	r := api.Report{Respondents: 7, Audience: 9, Responses: 30, Average: 2.1,
		Kudos: api.KudosSummary{Count: 5, Receivers: 3}}

	msg := teamSummary(es, g, "2026-11-09", r, round, "https://happyx.example/#/game/1?history")
	if msg == nil {
		t.Fatal("expected a summary")
	}
	d := msg.Embeds[0].Description
	for _, want := range []string{"Equipo · semana del", "«¿Tu primer trabajo?»", "**5 kudos** a 3 personas", "**7 de 9** personas"} {
		if !strings.Contains(d, want) {
			t.Errorf("summary should contain %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "2,1") || strings.Contains(d, "ánimo") {
		t.Errorf("the summary never tells the mood:\n%s", d)
	}
	if bs := reportButtons(msg); len(bs) != 1 || bs[0].URL == "" {
		t.Errorf("summary should link the game's answers: %+v", bs)
	}

	few := r
	few.Respondents = 4
	if d := teamSummary(es, g, "2026-11-09", few, round, "").Embeds[0].Description; strings.Contains(d, "respondieron") {
		t.Errorf("with fewer than %d people, how many answered stays out:\n%s", summaryMinPeople, d)
	}
	strict := g
	strict.AnonymityThreshold = 8
	if d := teamSummary(es, strict, "2026-11-09", r, round, "").Embeds[0].Description; strings.Contains(d, "respondieron") {
		t.Errorf("the server's higher minimum applies:\n%s", d)
	}
	off := false
	paused := g
	paused.SurveyEnabled = &off
	if d := teamSummary(es, paused, "2026-11-09", r, round, "").Embeds[0].Description; strings.Contains(d, "respondieron") {
		t.Errorf("with the survey off, no participation line:\n%s", d)
	}

	open := *round
	open.Phase = "guessing"
	if d := teamSummary(es, g, "2026-11-09", r, &open, "").Embeds[0].Description; strings.Contains(d, "Quién es quién") {
		t.Errorf("an unfinished game stays out:\n%s", d)
	}
	if teamSummary(es, g, "2026-11-09", api.Report{Respondents: 2}, nil, "") != nil {
		t.Error("nothing to say, no summary")
	}
}

func TestPreviousPeriod(t *testing.T) {
	for _, c := range []struct {
		from    string
		monthly bool
		pf, pt  string
	}{
		{"2026-11-09", false, "2026-11-02", "2026-11-08"},
		{"2026-11-01", true, "2026-10-01", "2026-10-31"},
		{"2026-03-01", true, "2026-02-01", "2026-02-28"},
		{"2026-01-01", true, "2025-12-01", "2025-12-31"},
	} {
		f, to, ok := previousPeriod(c.from, c.monthly)
		if !ok || f != c.pf || to != c.pt {
			t.Errorf("previousPeriod(%s, %v) = %s..%s, want %s..%s", c.from, c.monthly, f, to, c.pf, c.pt)
		}
	}
	if _, _, ok := previousPeriod("nope", false); ok {
		t.Error("a bad date has no period before")
	}
}

// The thanks card links what an admin told the team, and stays as it was when nobody did.
func TestWithTold(t *testing.T) {
	var asked string
	note := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path + "?" + r.URL.RawQuery
		if !note {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"not found"}`)
			return
		}
		io.WriteString(w, `{"id":3,"author_discord_user_id":"300000000000000001","author_username":"alex","from":"2026-11-02","to":"2026-11-08","body":"Martes sin reuniones","channel_id":"200000000000000001","message_id":"400000000000000001","posted_on":"2026-11-13"}`)
	}))
	defer srv.Close()
	dg, _ := discordgo.New("Bot x")
	b := &Bot{api: api.New(srv.URL, "key"), dg: dg, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now}
	es := i18n.Get("es")
	id := surveyID{GuildID: "100000000000000001", Day: "2026-11-16", Mood: 4}

	data := b.withTold(context.Background(), es, id, answeredMessage(es, api.Guild{Name: "Equipo"}, id, "Iván", ""))
	if asked != "/v1/guilds/100000000000000001/team-notes/current?day=2026-11-16" {
		t.Errorf("asked %q", asked)
	}
	row := data.Components[0].(discordgo.ActionsRow).Components
	last := row[len(row)-1].(discordgo.Button)
	if len(row) != 3 || last.Label != "Ver lo que contó alex" ||
		last.URL != "https://discord.com/channels/100000000000000001/200000000000000001/400000000000000001" {
		t.Fatalf("thanks card should link the note: %+v", row)
	}

	note = false
	data = b.withTold(context.Background(), es, id, answeredMessage(es, api.Guild{Name: "Equipo"}, id, "Iván", ""))
	if n := len(data.Components[0].(discordgo.ActionsRow).Components); n != 2 {
		t.Fatalf("without a note the card keeps its %d buttons, got %d", 2, n)
	}
}
