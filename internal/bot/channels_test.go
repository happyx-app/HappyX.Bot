package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestChannelListFollowsDiscordOrder(t *testing.T) {
	chans := []*discordgo.Channel{
		{ID: "10", Name: "Team", Type: discordgo.ChannelTypeGuildCategory, Position: 1},
		{ID: "11", Name: "Info", Type: discordgo.ChannelTypeGuildCategory, Position: 0},
		{ID: "20", Name: "kudos", Type: discordgo.ChannelTypeGuildText, ParentID: "10", Position: 0},
		{ID: "21", Name: "news", Type: discordgo.ChannelTypeGuildNews, ParentID: "11", Position: 3},
		{ID: "22", Name: "rules", Type: discordgo.ChannelTypeGuildText, ParentID: "11", Position: 1},
		{ID: "23", Name: "general", Type: discordgo.ChannelTypeGuildText, Position: 5},
		{ID: "24", Name: "voice", Type: discordgo.ChannelTypeGuildVoice, ParentID: "10", Position: 1},
		{ID: "25", Name: "orphan", Type: discordgo.ChannelTypeGuildText, ParentID: "99", Position: 0},
	}
	got := channelList(chans, func(id string) bool { return id != "22" })
	want := []struct{ id, category string }{{"25", ""}, {"23", ""}, {"22", "Info"}, {"21", "Info"}, {"20", "Team"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Category != w.category || got[i].Position != i {
			t.Errorf("%d: got %+v, want %s in %q", i, got[i], w.id, w.category)
		}
	}
	if got[2].CanPost || !got[0].CanPost {
		t.Errorf("can_post: %+v", got)
	}
}

func TestChannelListSkipsHiddenChannels(t *testing.T) {
	chans := []*discordgo.Channel{
		{ID: "10", Name: "___hidden___", Type: discordgo.ChannelTypeGuildCategory, Position: 0, Flags: channelObfuscated},
		{ID: "20", Name: "___hidden___", Type: discordgo.ChannelTypeGuildText, ParentID: "10", Position: 0, Flags: channelObfuscated},
		{ID: "21", Name: "general", Type: discordgo.ChannelTypeGuildText, Position: 1},
		{ID: "22", Name: "news", Type: discordgo.ChannelTypeGuildNews, Position: 2, Flags: 1 << 1},
	}
	got := channelList(chans, func(string) bool { return true })
	if len(got) != 2 || got[0].ID != "21" || got[1].ID != "22" {
		t.Fatalf("got %+v, want general and news only", got)
	}
}
