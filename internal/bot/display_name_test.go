package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Joining and member updates send the API the name the server shows: the server nickname, else
// the Discord display name, else the username.
func TestMemberEventsSendDisplayName(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]any
	record := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		io.WriteString(w, `{"discord_user_id":"`+fUser+`","username":"ana.garcia_corp","active":true}`)
	}
	f := &fake{h: map[string]func(http.ResponseWriter, *http.Request){
		"PUT /v1/guilds/" + fGuild + "/members/" + fUser: record,
	}}
	b := fakeBot(t, f, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))

	user := &discordgo.User{ID: fUser, Username: "ana.garcia_corp", GlobalName: "Ana García"}
	b.onMemberAdd(b.dg, &discordgo.GuildMemberAdd{Member: &discordgo.Member{GuildID: fGuild, User: user}})
	b.onMemberUpdate(b.dg, &discordgo.GuildMemberUpdate{Member: &discordgo.Member{GuildID: fGuild, User: user, Nick: "Ana"}})
	noName := &discordgo.User{ID: fUser, Username: "ana.garcia_corp"}
	b.onMemberUpdate(b.dg, &discordgo.GuildMemberUpdate{Member: &discordgo.Member{GuildID: fGuild, User: noName}})

	mu.Lock()
	defer mu.Unlock()
	want := []string{"Ana García", "Ana García", "Ana", "ana.garcia_corp"} // join sends twice: opt-in, then roles
	if len(sent) != len(want) {
		t.Fatalf("sent %d requests: %v", len(sent), sent)
	}
	for i, w := range want {
		if sent[i]["display_name"] != w || sent[i]["username"] != "ana.garcia_corp" {
			t.Errorf("request %d: %v, want display_name %q", i, sent[i], w)
		}
	}
}
