package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// /happyx in a DM with the bot: status, pause, resume and vacation, the commands about the
// member's own survey, work where the survey arrives. A DM says nothing about servers, so when
// the member is in several with HappyX the bot asks which one. The others are about a server
// and its team, and say to use them there.

const dmPrefix = "hxd" // the server picker of /happyx status, pause or resume in a DM

// dmID is the custom ID of the server picker: the command it completes.
type dmID struct{ Command string }

func (d dmID) String() string { return dmPrefix + ":" + d.Command }

func parseDMID(v string) (dmID, bool) {
	p := strings.Split(v, ":")
	if len(p) != 2 || p[0] != dmPrefix {
		return dmID{}, false
	}
	switch p[1] {
	case cmdStatus, cmdPause, cmdResume:
		return dmID{Command: p[1]}, true
	}
	return dmID{}, false
}

// maxPickerServers is how many servers a select menu offers: Discord's limit.
const maxPickerServers = 25

// handleDMCommand runs /happyx sub in a DM.
func (b *Bot) handleDMCommand(ctx context.Context, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	user := interactionUser(i)
	l := b.userLocale(ctx, i)
	switch sub.Name {
	case cmdStatus, cmdPause, cmdResume, cmdVacation:
	default:
		b.ephemeral(i, l.T("dm.server_only"))
		return
	}
	guilds, err := b.api.UserGuilds(ctx, user.ID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	if len(guilds) == 0 {
		b.ephemeral(i, l.T("dm.no_servers"))
		return
	}
	if sub.Name == cmdVacation {
		// A vacation counts in every server: any of them reads the dates in its timezone.
		b.dmVacation(ctx, i, l, guilds[0], user, sub.Options)
		return
	}
	if len(guilds) == 1 {
		b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, b.dmAct(ctx, l, guilds[0], user, sub.Name))
		return
	}
	b.respond(i, discordgo.InteractionResponseChannelMessageWithSource, serverPicker(l, sub.Name, guilds))
}

// serverPicker asks which server /happyx command is about.
func serverPicker(l *i18n.Locale, command string, guilds []api.Guild) *discordgo.InteractionResponseData {
	opts := make([]discordgo.SelectMenuOption, 0, min(len(guilds), maxPickerServers))
	for _, g := range guilds[:min(len(guilds), maxPickerServers)] {
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(g.Name, 100), Value: g.DiscordID})
	}
	return &discordgo.InteractionResponseData{
		Content: l.T("dm.pick_" + command),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{CustomID: dmID{Command: command}.String(), Placeholder: l.T("dm.pick_placeholder"), Options: opts},
		}}},
		Flags:           discordgo.MessageFlagsEphemeral,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// handleDMPick runs the command once the member picked the server.
func (b *Bot) handleDMPick(ctx context.Context, i *discordgo.InteractionCreate, id dmID) {
	user := interactionUser(i)
	l := b.userLocale(ctx, i)
	values := i.MessageComponentData().Values
	if len(values) != 1 {
		return
	}
	guilds, err := b.api.UserGuilds(ctx, user.ID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	for _, g := range guilds {
		if g.DiscordID == values[0] {
			data := b.dmAct(ctx, l, g, user, id.Command)
			data.Flags = 0 // updating the ephemeral message keeps it ephemeral
			b.respond(i, discordgo.InteractionResponseUpdateMessage, data)
			return
		}
	}
	b.respond(i, discordgo.InteractionResponseUpdateMessage, &discordgo.InteractionResponseData{
		Content: l.T("dm.not_there"), Components: []discordgo.MessageComponent{},
	})
}

// dmAct runs /happyx status, pause or resume for g from a DM. The answer is in the member's
// language; the survey card it changes in the DM stays in the server's.
func (b *Bot) dmAct(ctx context.Context, l *i18n.Locale, g api.Guild, user *discordgo.User, command string) *discordgo.InteractionResponseData {
	gl := i18n.Get(g.Language)
	text := func(s string) *discordgo.InteractionResponseData {
		return &discordgo.InteractionResponseData{
			Content: s, Components: []discordgo.MessageComponent{}, Flags: discordgo.MessageFlagsEphemeral,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}
	}
	switch command {
	case cmdStatus:
		data, err := b.status(ctx, l, g, user, false)
		if err != nil {
			return text(b.errorText(l, err))
		}
		return data
	case cmdPause:
		if err := b.api.RemoveMember(ctx, g.DiscordID, user.ID); err != nil && !errors.Is(err, api.ErrNotFound) {
			return text(b.errorText(l, err))
		}
		b.syncSurveyCard(gl, g, user, true)
		return text(l.T("options.paused_cmd", "guild", g.Name, "resume_cmd", b.resumeMention()))
	default: // cmdResume
		ended, err := b.resume(ctx, g, user)
		if err != nil {
			return text(b.errorText(l, err))
		}
		b.syncSurveyCard(gl, g, user, false)
		return text(resumedText(l, g.Name, ended))
	}
}

// dmVacation is /happyx vacation in a DM.
func (b *Bot) dmVacation(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, g api.Guild, user *discordgo.User, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	var fromText, toText string
	for _, o := range opts {
		switch o.Name {
		case optFrom:
			fromText = o.StringValue()
		case optTo:
			toText = o.StringValue()
		}
	}
	text, _, err := b.setVacation(ctx, g, l, user, fromText, toText)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	b.ephemeral(i, text)
}
