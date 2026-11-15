package bot

import (
	"context"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// «Your week»: the thanks card of the week's last survey day shows the member their own answers
// of that week, one face per day, and links to their evolution on the web. Nobody else sees it,
// and it says nothing about streaks: a day without an answer is just a dash.

// weekLine is «Your week: mo 😐 · tu 🙂 · we – · th 🤩 · fr 🙂» for the survey days of week,
// with each day's answer from moods.
func weekLine(l *i18n.Locale, week []time.Time, moods map[string]int) string {
	names := l.List("date.weekdays") // Sunday first
	parts := make([]string, 0, len(week))
	for _, d := range week {
		face := "–"
		if m, ok := moods[d.Format(isoDate)]; ok {
			face = moodEmoji(m)
		}
		parts = append(parts, names[d.Weekday()]+" "+face)
	}
	return l.T("answer.week", "days", strings.Join(parts, " · "))
}

// withWeek adds «Your week» and the link to the member's evolution to the thanks card of an
// answer on the week's last survey day. If their answers can't be read, the card stays as it is.
func (b *Bot) withWeek(ctx context.Context, l *i18n.Locale, g api.Guild, id surveyID, userID string, data *discordgo.InteractionResponseData) *discordgo.InteractionResponseData {
	day := parseDay(id.Day)
	if !lastWorkingDay(g.WorkingDays, day) || len(data.Embeds) == 0 {
		return data
	}
	week := workingWeek(g.WorkingDays, day)
	entries, err := b.api.MemberMoods(ctx, g.DiscordID, userID, week[0].Format(isoDate), id.Day)
	if err != nil {
		b.log.Warn("member's week", "guild", g.DiscordID, "err", err)
		return data
	}
	moods := map[string]int{id.Day: id.Mood}
	for _, e := range entries {
		if e.Day != id.Day {
			moods[e.Day] = e.Mood
		}
	}
	return addWeek(l, data, weekLine(l, week, moods), b.evolutionURL(g.DiscordID))
}

// addWeek puts the week's line at the end of the thanks card and the link to the evolution after
// its buttons.
func addWeek(l *i18n.Locale, data *discordgo.InteractionResponseData, line, link string) *discordgo.InteractionResponseData {
	data.Embeds[0].Description += "\n\n" + line
	if link == "" || len(data.Components) == 0 {
		return data
	}
	if row, ok := data.Components[0].(discordgo.ActionsRow); ok {
		row.Components = append(row.Components, discordgo.Button{
			Label: l.T("options.evolution_button"), Emoji: &discordgo.ComponentEmoji{Name: "📈"}, Style: discordgo.LinkButton, URL: link,
		})
		data.Components[0] = row
	}
	return data
}

// thanks is the card an answer turns the survey into: the answer, the link to what an admin told
// the team, and on the week's last survey day the member's week.
func (b *Bot) thanks(ctx context.Context, l *i18n.Locale, g api.Guild, id surveyID, user *discordgo.User, comment string) *discordgo.InteractionResponseData {
	data := answeredMessage(l, g, id, b.userName(id.GuildID, user), comment)
	return b.withWeek(ctx, l, g, id, user.ID, b.withTold(ctx, l, id, data))
}
