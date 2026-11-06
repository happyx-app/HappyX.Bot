package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

const isoDate = "2006-01-02"

// maxVacationDays is the API's limit, both ends counted; checked here too so the reply is in the
// member's language.
const maxVacationDays = 90

var (
	isoDateRe = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	// Day first, as written in every language HappyX speaks but American English: 20/12,
	// 20-12-2026, 20.12.26.
	dayMonthRe = regexp.MustCompile(`^(\d{1,2})[/.\-](\d{1,2})(?:[/.\-](\d{2}|\d{4}))?$`)
)

// parseVacationDate reads a date the way people type it. Without a year it is the one closest
// after notBefore, or a recent one: «20/12» or «2/11» typed on 6 November are this year, «6/1» is
// next January.
func parseVacationDate(s string, notBefore time.Time) (time.Time, bool) {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".") // «20.12.» in German
	var y, m, d int
	if p := isoDateRe.FindStringSubmatch(s); p != nil {
		y, _ = strconv.Atoi(p[1])
		m, _ = strconv.Atoi(p[2])
		d, _ = strconv.Atoi(p[3])
	} else if p := dayMonthRe.FindStringSubmatch(s); p != nil {
		d, _ = strconv.Atoi(p[1])
		m, _ = strconv.Atoi(p[2])
		if p[3] != "" {
			y, _ = strconv.Atoi(p[3])
			if y < 100 {
				y += 2000
			}
		}
	} else {
		return time.Time{}, false
	}
	guess := y == 0
	if guess {
		y = notBefore.Year()
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != m { // 31/2 and the like
		return time.Time{}, false
	}
	// A vacation that started up to maxVacationDays ago may still be on, so those days stay in
	// this year; earlier ones mean next year.
	if guess && t.Before(notBefore.AddDate(0, 0, -maxVacationDays)) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// discordDate shows a day in each reader's own language and format. Noon UTC stays the same
// day in every timezone people work in.
func discordDate(day string) string {
	t, err := time.Parse(isoDate, day)
	if err != nil {
		return day
	}
	return fmt.Sprintf("<t:%d:D>", t.Add(12*time.Hour).Unix())
}

// guildToday is today's date in the server's timezone.
func guildToday(g api.Guild, now time.Time) time.Time {
	loc, err := time.LoadLocation(g.Timezone)
	if err != nil {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// cmdVacation is /happyx vacation from to: the member's own vacation, for every server.
func (b *Bot) cmdVacation(ctx context.Context, i *discordgo.InteractionCreate, l *i18n.Locale, user *discordgo.User, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	g, err := b.api.Guild(ctx, i.GuildID)
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	today := guildToday(g, b.now())
	var fromText, toText string
	for _, o := range opts {
		switch o.Name {
		case optFrom:
			fromText = o.StringValue()
		case optTo:
			toText = o.StringValue()
		}
	}
	from, ok := parseVacationDate(fromText, today)
	if !ok {
		b.ephemeral(i, l.T("vacation.bad_date", "date", fromText))
		return
	}
	to, ok := parseVacationDate(toText, from)
	if !ok {
		b.ephemeral(i, l.T("vacation.bad_date", "date", toText))
		return
	}
	if to.Before(from) || to.Before(today) || to.Sub(from) >= maxVacationDays*24*time.Hour {
		b.ephemeral(i, l.T("vacation.invalid", "max", maxVacationDays))
		return
	}
	v, err := b.api.SetVacation(ctx, i.GuildID, user.ID, from.Format(isoDate), to.Format(isoDate))
	if err != nil {
		b.replyError(i, l, err)
		return
	}
	b.ephemeral(i, l.T("vacation.done", "from", discordDate(v.From), "to", discordDate(v.To), "resume_cmd", b.resumeMention()))
}

// vacationStatus is what /happyx status says about the member's vacations: the lines to add and
// whether one covers today.
func vacationStatus(l *i18n.Locale, vs []api.Vacation, today time.Time) (lines string, away bool) {
	day := today.Format(isoDate)
	for _, v := range vs {
		lines += "\n" + l.T("status.vacation", "from", discordDate(v.From), "to", discordDate(v.To))
		if v.From <= day && day <= v.To {
			away = true
		}
	}
	return lines, away
}

// notifyVacations tells members by DM about the vacations an admin set for them on the
// dashboard, in the server's language. A member with closed DMs isn't asked again.
func (b *Bot) notifyVacations(ctx context.Context) {
	ns, err := b.api.VacationNotices(ctx)
	if err != nil {
		if !errors.Is(err, api.ErrNotFound) { // an API from before vacations
			b.log.Error("list vacation notices", "err", err)
		}
		return
	}
	for _, n := range ns {
		g, err := b.api.Guild(ctx, n.GuildID)
		if err != nil {
			b.log.Error("vacation notice guild", "guild", n.GuildID, "err", err)
			continue
		}
		l := i18n.Get(g.Language)
		link := "<" + b.webURL + "/#/me>"
		if b.webURL == "" {
			link = b.resumeMention()
		}
		msg := &discordgo.MessageSend{
			Content: l.T("vacation.notice", "admin", "<@"+n.SetByID+">", "server", g.Name,
				"from", discordDate(n.From), "to", discordDate(n.To), "link", link),
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}
		if err := b.dm(n.UserID, msg); err != nil {
			b.log.Warn("vacation notice DM", "guild", n.GuildID, "user", n.UserID, "err", err)
		}
		if err := b.api.DoneVacationNotice(ctx, n.ID); err != nil && !errors.Is(err, api.ErrNotFound) {
			b.log.Error("vacation notice done", "id", n.ID, "err", err)
		}
	}
}
