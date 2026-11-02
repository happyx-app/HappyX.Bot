package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

func TestSetupIDRoundTrip(t *testing.T) {
	id := setupID{Action: setupRoles, GuildID: "100000000000000001", Page: "1e"}
	got, ok := parseSetupID(id.String())
	if !ok || got != id {
		t.Fatalf("parse(%q) = %+v %v", id.String(), got, ok)
	}
	for _, bad := range []string{"hxs:roles:abc:1", "hxs:roles:1:9", "hx:mood:1:2026-01-01:3", "hxs:roles:1"} {
		if _, ok := parseSetupID(bad); ok {
			t.Errorf("parse(%q) accepted", bad)
		}
	}
	if g, ok := parseTryID(testID{GuildID: "1"}.tryString()); !ok || g != "1" {
		t.Errorf("try ID: %q %v", g, ok)
	}
	if _, ok := parseTestID(testID{GuildID: "1"}.tryString()); ok {
		t.Error("the try button parsed as a mood")
	}
	if got, ok := parseTestID(testID{GuildID: "1", Mood: 5}.String()); !ok || got.Mood != 5 {
		t.Errorf("test ID: %+v %v", got, ok)
	}
}

func TestSetupPatch(t *testing.T) {
	g := "100000000000000001"
	// Choosing «only these roles» waits for the roles; nothing is saved yet.
	_, page, save, ok := setupPatch(setupID{Action: setupWho, GuildID: g, Page: "1"}, []string{api.RolesOnly})
	if !ok || save || page != "1o" {
		t.Fatalf("who=only: page %q save %v ok %v", page, save, ok)
	}
	p, page, save, _ := setupPatch(setupID{Action: setupRoles, GuildID: g, Page: "1e"}, []string{"5", "6"})
	if !save || page != "1" || p.SurveyRoles.Mode != api.RolesExcept || len(p.SurveyRoles.RoleIDs) != 2 {
		t.Fatalf("roles on 1e: %+v page %q", p.SurveyRoles, page)
	}
	// Clearing every role means everyone again.
	p, _, _, _ = setupPatch(setupID{Action: setupGameRoles, GuildID: g, Page: "2o"}, nil)
	if p.GameRoles.Mode != api.RolesEveryone || p.GameRoles.RoleIDs == nil {
		t.Fatalf("no roles: %+v", p.GameRoles)
	}
	p, _, _, _ = setupPatch(setupID{Action: setupKudos, GuildID: g, Page: "2"}, []string{"-"})
	if p.KudosChannelID == nil || *p.KudosChannelID != "" {
		t.Fatalf("kudos here: %+v", p.KudosChannelID)
	}
	p, _, _, _ = setupPatch(setupID{Action: setupDays, GuildID: g, Page: "1"}, []string{"sun_thu"})
	if len(p.WorkingDays) != 5 || p.WorkingDays[0] != 7 {
		t.Fatalf("days: %v", p.WorkingDays)
	}
	for _, c := range []struct{ action, v string }{{setupHour, "24"}, {setupDays, "custom"}, {setupFrequency, "daily"}, {setupWho, "some"}} {
		if _, _, _, ok := setupPatch(setupID{Action: c.action, GuildID: g, Page: "1"}, []string{c.v}); ok {
			t.Errorf("%s=%s accepted", c.action, c.v)
		}
	}
}

// Discord takes at most 40 components in a Components v2 message, nested ones included.
func TestSetupPagesFitDiscord(t *testing.T) {
	for _, code := range i18n.Codes() {
		for _, p := range setupPreviews(i18n.Get(code), api.Guild{DiscordID: PreviewGuildID, Name: "x", Language: code, Timezone: "Europe/Madrid"}) {
			if n := countComponents(p.Msg.Components); n > 40 {
				t.Errorf("%s %q: %d components", code, p.Title, n)
			}
		}
	}
}

func countComponents(cs []discordgo.MessageComponent) int {
	n := 0
	for _, c := range cs {
		n++
		switch c := c.(type) {
		case discordgo.Container:
			n += countComponents(c.Components)
		case discordgo.ActionsRow:
			n += countComponents(c.Components)
		}
	}
	return n
}

func TestRoleSelectSkipsEveryoneAndBots(t *testing.T) {
	roles := []*discordgo.Role{{ID: "1", Name: "@everyone"}, {ID: "2", Name: "Bot", Managed: true}}
	for i := 0; i < 30; i++ {
		roles = append(roles, &discordgo.Role{ID: string(rune('a' + i)), Name: "r", Position: i})
	}
	chosen := api.RoleFilter{Mode: api.RolesOnly, RoleIDs: []string{"a"}} // the lowest role
	out := roleSelect(i18n.Get("en"), "id", roles, chosen, "1")
	menu := out[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if len(menu.Options) != maxOptions || len(out) != 2 {
		t.Fatalf("%d options, %d components", len(menu.Options), len(out))
	}
	if menu.Options[0].Value != "a" || !menu.Options[0].Default {
		t.Errorf("the chosen role must stay in the list: %+v", menu.Options[0])
	}
	for _, o := range menu.Options {
		if o.Value == "1" || o.Value == "2" {
			t.Errorf("listed %q", o.Value)
		}
	}
}
