package bot

import (
	"cmp"
	"context"
	"slices"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
)

// channelObfuscated is the flag Discord sets, over the gateway, on a channel the bot can't view.
// Its name then reads "___hidden___", so the channel is useless to list. From 2026-11-16 it
// applies to every bot; the HTTP API leaves such channels out altogether.
const channelObfuscated = 1 << 17

// kudosPermissions is what posting the kudos card takes.
const kudosPermissions = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionEmbedLinks

// syncChannels sends the server's text and announcement channels, as the session's state holds
// them, so the dashboard can offer them for the kudos channel. It runs on connect and after any
// change to channels, roles or the bot's own roles, since those change where it may post.
func (b *Bot) syncChannels(ctx context.Context, guildID string) {
	g, err := b.dg.State.Guild(guildID)
	if err != nil {
		b.log.Error("channels from state", "guild", guildID, "err", err)
		return
	}
	me := b.dg.State.User
	out := channelList(g.Channels, func(channelID string) bool {
		if me == nil {
			return true
		}
		perms, err := b.dg.State.UserChannelPermissions(me.ID, channelID)
		if err != nil {
			return true // unknown: the kudos card falls back to the command's channel anyway
		}
		return perms&kudosPermissions == kudosPermissions
	})
	if err := b.api.SyncChannels(ctx, guildID, out); err != nil {
		b.log.Error("sync channels", "guild", guildID, "err", err)
	}
}

// channelList keeps the text and announcement channels in the order Discord's sidebar shows
// them: those outside a category first, then each category's, categories in their own order.
// Channels hidden from the bot are left out: their names are not real, and a kudos channel the
// bot can't see goes back to posting where the command is used.
func channelList(channels []*discordgo.Channel, canPost func(channelID string) bool) []api.Channel {
	byPosition := func(a, b *discordgo.Channel) int {
		return cmp.Or(cmp.Compare(a.Position, b.Position), cmp.Compare(len(a.ID), len(b.ID)), cmp.Compare(a.ID, b.ID))
	}
	var categories, texts []*discordgo.Channel
	for _, c := range channels {
		if c.Flags&channelObfuscated != 0 {
			continue
		}
		switch c.Type {
		case discordgo.ChannelTypeGuildCategory:
			categories = append(categories, c)
		case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
			texts = append(texts, c)
		}
	}
	slices.SortFunc(categories, byPosition)
	slices.SortFunc(texts, byPosition)
	rank := map[string]int{"": 0} // no category comes first
	names := map[string]string{}
	for i, c := range categories {
		rank[c.ID], names[c.ID] = i+1, c.Name
	}
	slices.SortStableFunc(texts, func(a, b *discordgo.Channel) int {
		return cmp.Compare(rank[parentOf(a, rank)], rank[parentOf(b, rank)])
	})
	out := make([]api.Channel, 0, len(texts))
	for i, c := range texts {
		out = append(out, api.Channel{ID: c.ID, Name: c.Name, Category: names[parentOf(c, rank)], Position: i, CanPost: canPost(c.ID)})
	}
	return out
}

// parentOf is the channel's category, or "" when it has none the bot can see.
func parentOf(c *discordgo.Channel, known map[string]int) string {
	if _, ok := known[c.ParentID]; ok {
		return c.ParentID
	}
	return ""
}
