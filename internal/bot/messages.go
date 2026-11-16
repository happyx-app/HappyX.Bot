package bot

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
	"github.com/happyx-app/HappyX.Bot/internal/report"
)

// moodEmojis are the 1-5 answers, worst first. Their labels are mood.1-mood.5 in the catalogs.
var moodEmojis = []string{"😞", "😕", "😐", "🙂", "🤩"}

func moodEmoji(m int) string {
	if m < 1 || m > len(moodEmojis) {
		return "❔"
	}
	return moodEmojis[m-1]
}

// surveyRecipient is who a survey DM goes to, for the custom message's {{user.*}} variables.
type surveyRecipient struct {
	ID     string
	Name   string // their name in the server
	Streak int    // days in a row they answered
	// PolicyURL is the privacy policy, set until they first answer since its date (the API's
	// policy_pending): the survey then carries a link button to it.
	PolicyURL string
	// Yesterday is set when the survey shown is yesterday's, still open: after midnight its
	// question asks about yesterday, not today.
	Yesterday bool
}

// surveyColor is HappyX's violet, the colour of the survey card until it's answered.
const surveyColor = 0x8b5cf6

// surveyMessage is the daily survey: a card with the question (or the admin's own message), the
// anonymity notice, and the mood buttons (just their emoji) below. icon is the server's icon, if any.
func surveyMessage(l *i18n.Locale, g api.Guild, day string, to surveyRecipient, icon string) *discordgo.MessageSend {
	buttons := moodButtons(g, day, actionMood)
	privacy := surveyPrivacy(l, g, day)
	embed := &discordgo.MessageEmbed{
		Author:      &discordgo.MessageEmbedAuthor{Name: l.T("survey.title", "guild", g.Name)},
		Description: surveyText(l, g, day, to),
		Color:       surveyColor,
		Footer:      &discordgo.MessageEmbedFooter{Text: privacy},
	}
	if icon != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: icon}
	}
	more := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    l.T("survey.options_button"),
			Emoji:    &discordgo.ComponentEmoji{Name: "⚙️"},
			Style:    discordgo.SecondaryButton,
			CustomID: surveyID{Action: actionOptions, GuildID: g.DiscordID, Day: day}.String(),
		},
	}
	if to.PolicyURL != "" {
		more = append(more, discordgo.Button{Label: l.T("survey.policy_button"), Style: discordgo.LinkButton, URL: to.PolicyURL})
	}
	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		// A custom message may hold @everyone or role mentions; they're shown but never ping.
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: buttons},
			discordgo.ActionsRow{Components: more},
		},
	}
}

// moodButtons are the five faces, just their emoji, recording the mood with action.
func moodButtons(g api.Guild, day, action string) []discordgo.MessageComponent {
	buttons := make([]discordgo.MessageComponent, 0, len(moodEmojis))
	for i, emoji := range moodEmojis {
		buttons = append(buttons, discordgo.Button{
			Emoji:    &discordgo.ComponentEmoji{Name: emoji},
			Style:    discordgo.SecondaryButton,
			CustomID: surveyID{Action: action, GuildID: g.DiscordID, Day: day, Mood: i + 1}.String(),
		})
	}
	return buttons
}

// changeMessage is what «Change answer» turns the answer into: the survey again, with the
// comment quoted below the question, since choosing another face keeps it, and only the faces.
// Those faces carry their own action so that, left unchosen, the card still counts as answered.
func changeMessage(l *i18n.Locale, g api.Guild, day string, to surveyRecipient, icon, quoted string) *discordgo.InteractionResponseData {
	msg := surveyMessage(l, g, day, to, icon)
	if quoted != "" {
		msg.Embeds[0].Description += "\n" + quoted
	}
	return &discordgo.InteractionResponseData{
		Content:         "",
		Embeds:          msg.Embeds,
		Components:      []discordgo.MessageComponent{discordgo.ActionsRow{Components: moodButtons(g, day, actionRemood)}},
		AllowedMentions: msg.AllowedMentions,
	}
}

// answeredQuote is the comment quoted on an answer card from answeredMessage, as it is shown
// there, or "" if the card has none.
func answeredQuote(m *discordgo.Message) string {
	if m == nil || len(m.Embeds) == 0 {
		return ""
	}
	d := m.Embeds[0].Description
	i := strings.Index(d, "\n> ")
	if i < 0 {
		return ""
	}
	// The quote's lines all start with "> "; whatever follows them, like «Your week», isn't it.
	var lines []string
	for _, line := range strings.Split(d[i+1:], "\n") {
		if !strings.HasPrefix(line, "> ") {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// noButtons keeps a message as it is but takes its buttons away.
func noButtons() *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{Components: []discordgo.MessageComponent{}}
}

// namedNoticeDays is how long, after anonymity is turned off, the survey says since when the
// admins see who answers.
const namedNoticeDays = 7

// surveyPrivacy is the survey's footer: whether this day's answer is anonymous. Around the day
// anonymity is turned off it also says from which day answers carry names, so the team learns it
// from the survey itself before answering.
func surveyPrivacy(l *i18n.Locale, g api.Guild, day string) string {
	switch {
	case g.NamedSoon(day):
		return l.T("survey.anonymous_until", "date", shortDate(l, g.NamedSince))
	case g.AnonymousOn(day):
		return l.T("survey.anonymous")
	case g.NamedSince != "" && day < parseDay(g.NamedSince).AddDate(0, 0, namedNoticeDays).Format(isoDate):
		return l.T("survey.named_since", "date", shortDate(l, g.NamedSince))
	}
	return l.T("survey.named")
}

var surveyVar = regexp.MustCompile(`\{\{\s*([a-z.]+)\s*\}\}`)

// surveyText is the survey's question: the admin's own message with its variables filled in for
// this person, as written in whatever language they wrote it, or the default text when there is
// none: a greeting by name and the question. The API only accepts known variables, but an
// unknown one is left as written.
func surveyText(l *i18n.Locale, g api.Guild, day string, to surveyRecipient) string {
	if strings.TrimSpace(g.SurveyMessage) == "" {
		question := l.T("survey.question")
		if to.Yesterday {
			question = l.T("survey.question_yesterday")
		}
		return "### " + l.T("survey.greeting", "name", to.Name) + "\n" + question
	}
	d := parseDay(day)
	vars := map[string]string{
		"user.name":    to.Name,
		"user.mention": "<@" + to.ID + ">",
		"user.streak":  strconv.Itoa(to.Streak),
		"guild.name":   g.Name,
		"date":         longDate(l, d),
		"weekday":      l.List("date.weekdays_long")[d.Weekday()],
	}
	return surveyVar.ReplaceAllStringFunc(g.SurveyMessage, func(m string) string {
		if v, ok := vars[surveyVar.FindStringSubmatch(m)[1]]; ok {
			return v
		}
		return m
	})
}

// parseDay reads a YYYY-MM-DD day, today if it can't.
func parseDay(day string) time.Time {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Now()
	}
	return d
}

// longDate is d in full, like «viernes, 24 de octubre».
func longDate(l *i18n.Locale, d time.Time) string {
	return l.T("date.long", "weekday", l.List("date.weekdays_long")[d.Weekday()], "day", d.Day(),
		"month", l.List("date.months_long")[d.Month()-1])
}

// answeredMessage replaces the survey once answered: the mood in the card's colour, the comment
// if there is one, and buttons to add a comment or change the mood.
func answeredMessage(l *i18n.Locale, g api.Guild, id surveyID, name, comment string) *discordgo.InteractionResponseData {
	text := "### " + l.T("answer.thanks", "emoji", moodEmoji(id.Mood), "name", name) + "\n" +
		l.T("answer.saved", "mood", l.T(fmt.Sprintf("mood.%d", id.Mood)))
	change := id
	change.Action = actionChange
	row := []discordgo.MessageComponent{
		discordgo.Button{Label: l.T("answer.change_button"), Style: discordgo.SecondaryButton, CustomID: change.String()},
	}
	if comment == "" {
		note := id
		note.Action = actionNote
		row = append([]discordgo.MessageComponent{
			discordgo.Button{Label: l.T("answer.comment_button"), Emoji: &discordgo.ComponentEmoji{Name: "💬"}, Style: discordgo.PrimaryButton, CustomID: note.String()},
		}, row...)
	} else {
		text += "\n" + quote(comment)
	}
	return &discordgo.InteractionResponseData{
		Content: "", // surveys sent before the card had their text here
		Embeds: []*discordgo.MessageEmbed{{
			Author:      &discordgo.MessageEmbedAuthor{Name: l.T("survey.title", "guild", g.Name)},
			Description: text,
			Color:       moodColor(api.Report{Responses: 1, Average: float64(id.Mood)}),
		}},
		Components:      []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

// closedColor is Discord's grey, for a survey that can no longer be answered or is paused.
const closedColor = 0x4e5058

// closedSurvey is a survey that can no longer be answered: just its server and day, in grey.
func closedSurvey(l *i18n.Locale, g api.Guild, day string) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		Content:         "",
		Embeds:          []*discordgo.MessageEmbed{closedEmbed(l, g.Name, day)},
		Components:      []discordgo.MessageComponent{},
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	}
}

func closedEmbed(l *i18n.Locale, guildName, day string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{Name: l.T("survey.closed_title", "guild", guildName, "date", longDate(l, parseDay(day)))},
		Color:  closedColor,
	}
}

// noteModal asks for the comment and says who reads it.
func noteModal(l *i18n.Locale, g api.Guild, id surveyID) *discordgo.InteractionResponseData {
	id.Action = actionNoteModal
	label := l.T("modal.label_anonymous")
	if !g.AnonymousOn(id.Day) {
		label = l.T("modal.label_named")
	}
	return &discordgo.InteractionResponseData{
		CustomID: id.String(),
		Title:    l.T("modal.title"),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.TextInput{
				CustomID:    "comment",
				Label:       label,
				Style:       discordgo.TextInputParagraph,
				Required:    true,
				MaxLength:   500,
				Placeholder: l.T("modal.placeholder"),
			},
		}}},
	}
}

// Discord limits: 4096 characters in an embed description and 6000 in the whole message. The
// comments and kudos get budgets well under that, so a busy server's report still sends.
const (
	kudosMax      = 150 // characters of each kudos shown; there are at most 5
	commentBudget = 2700
	commentMax    = 300
	maxComments   = 20 // more than this is a wall of text; the rest are counted
)

// reportOptions is what a report adds when it can: the period before, to compare the average
// with, and the buttons to tell the team and to open the dashboard.
type reportOptions struct {
	// Previous is the report of the period before; nil leaves the comparison out.
	Previous *api.Report
	// Monthly says the periods are months, not weeks.
	Monthly bool
	// DashboardURL opens the period on the web; empty leaves the button out.
	DashboardURL string
	// Tell adds «Tell the team», which publishes in the team channel; it needs one.
	Tell bool
}

// reportMessage sends the report as an image, with a short summary and the comments as text
// (they can be long and are easier to read that way). The embed colour follows the average mood.
// If the image can't be rendered, the summary also carries the distribution.
func reportMessage(l *i18n.Locale, g api.Guild, r api.Report) *discordgo.MessageSend {
	return reportMessageWith(l, g, r, reportOptions{})
}

func reportMessageWith(l *i18n.Locale, g api.Guild, r api.Report, o reportOptions) *discordgo.MessageSend {
	embed := &discordgo.MessageEmbed{
		Title:  fmt.Sprintf("HappyX · %s", g.Name),
		Footer: &discordgo.MessageEmbedFooter{Text: l.T("report.period", "from", shortDate(l, r.From), "to", shortDate(l, r.To))},
		Color:  moodColor(r),
	}
	msg := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}}
	var buttons []discordgo.MessageComponent
	if o.Tell && !r.Withheld && r.Responses > 0 && g.TeamChannel() != "" {
		buttons = append(buttons, discordgo.Button{
			Label: l.T("report.tell_button"), Emoji: &discordgo.ComponentEmoji{Name: "💬"}, Style: discordgo.PrimaryButton,
			CustomID: teamID{Action: teamTell, GuildID: g.DiscordID, From: r.From, To: r.To}.String(),
		})
	}
	if o.DashboardURL != "" {
		buttons = append(buttons, discordgo.Button{Label: l.T("setup.open_web"), Style: discordgo.LinkButton, URL: o.DashboardURL})
	}
	if len(buttons) > 0 {
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
	}
	if r.Withheld {
		// Every answer fell on a day with too few people to show more than how many: no image, no comments.
		embed.Color = surveyColor
		embed.Description = l.T("report.withheld", "n", r.Respondents, "k", max(r.AnonymityThreshold, api.MinAnonymityThreshold)) +
			kudosSection(l, r.Kudos)
		return msg
	}
	if png, err := report.Render(l, g.Name, r); err == nil {
		msg.Files = []*discordgo.File{{Name: "report.png", ContentType: "image/png", Reader: bytes.NewReader(png)}}
		embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://report.png"}
		embed.Description = summary(l, r, o)
	} else {
		embed.Description = textSummary(l, r, o)
	}
	embed.Description += kudosSection(l, r.Kudos) + comments(l, r.Messages)
	return msg
}

// summary is what an admin reads first: the average and how it moved since the period before,
// how many of the team answered, and how many answers were bad, the ones to act on.
func summary(l *i18n.Locale, r api.Report, o reportOptions) string {
	if r.Responses == 0 {
		return l.T("report.no_responses")
	}
	avg := l.T("report.average_line", "value", l.Decimal(r.Average), "emoji", moodEmoji(int(r.Average+0.5)))
	if change := comparison(l, r, o); change != "" {
		avg += " · " + change
	}
	lines := []string{avg, answered(l, r)}
	if n := r.Counted(); n > 0 {
		bad := r.Distribution["1"] + r.Distribution["2"]
		lines = append(lines, l.T("report.low", "bad", l.T("mood.2"), "very_bad", l.T("mood.1"), "n", bad, "of", n))
	}
	return strings.Join(lines, "\n") + withheldDays(l, r)
}

// comparison says how the average moved since the period before, rounded as it is shown. Empty
// when either period has no average.
func comparison(l *i18n.Locale, r api.Report, o reportOptions) string {
	p := o.Previous
	if p == nil || p.Withheld || p.Counted() == 0 || r.Counted() == 0 {
		return ""
	}
	key := "report.vs_week"
	if o.Monthly {
		key = "report.vs_month"
	}
	delta := math.Round(r.Average*10)/10 - math.Round(p.Average*10)/10
	switch {
	case math.Abs(delta) < 0.05:
		return l.T(key + "_same")
	case delta > 0:
		return l.T(key, "delta", "+"+l.Decimal(delta))
	default:
		return l.T(key, "delta", "−"+l.Decimal(-delta))
	}
}

// answered is how many people answered, out of those who get the survey when that is known and
// makes sense (people who left since may have answered too).
func answered(l *i18n.Locale, r api.Report) string {
	switch {
	case r.Audience >= r.Respondents && r.Audience > 0:
		return l.T("report.answered_of", "n", r.Respondents, "of", r.Audience)
	case r.Respondents == 1:
		return l.T("report.answered_one")
	default:
		return l.T("report.answered", "n", r.Respondents)
	}
}

// withheldDays names the days with too few people, which count only in the answers, one line
// per threshold they were judged by (they differ only after the guild lowered it). Empty when
// there are none.
func withheldDays(l *i18n.Locale, r api.Report) string {
	byK := map[int][]string{}
	var ks []int
	for _, d := range r.Days {
		if !d.Withheld {
			continue
		}
		k := d.AnonymityThreshold
		if k == 0 { // older APIs
			k = r.AnonymityThreshold
		}
		k = max(k, api.MinAnonymityThreshold)
		if byK[k] == nil {
			ks = append(ks, k)
		}
		byK[k] = append(byK[k], dayLabel(l, d.Day))
	}
	slices.Sort(ks)
	var b strings.Builder
	for _, k := range ks {
		b.WriteString("\n-# " + l.T("report.withheld_days", "days", strings.Join(byK[k], ", "), "k", k))
	}
	return b.String()
}

// comments lists the comments as quotes, stopping at the budget with a count of the rest. An
// anonymous comment is only its text, in the order the API gives (unrelated to when it was
// written); a named one has its mood, day and author.
func comments(l *i18n.Locale, msgs []api.ReportMsg) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n### 💬 " + l.T("report.comments") + "\n")
	for i, m := range msgs {
		entry := quote(truncate(m.Message, commentMax)) + "\n"
		if i > 0 && msgs[i-1].Day == "" {
			entry = "\n" + entry // a blank line keeps two quotes from running into one
		}
		if m.Day != "" {
			meta := dayLabel(l, m.Day)
			if m.Username != "" {
				meta += " · " + m.Username
			}
			entry = fmt.Sprintf("> %s %s\n-# %s\n", moodEmoji(m.Mood),
				strings.ReplaceAll(truncate(m.Message, commentMax), "\n", "\n> "), meta)
		}
		if i == maxComments || b.Len()+len(entry) > commentBudget {
			b.WriteString("-# " + l.T("report.more_comments", "n", len(msgs)-i) + "\n")
			break
		}
		b.WriteString(entry)
	}
	return b.String()
}

// kudosSection counts the period's kudos and quotes the latest. Kudos are public, so they keep
// their names even in anonymous servers.
func kudosSection(l *i18n.Locale, k api.KudosSummary) string {
	if k.Count == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n### 🙌 " + l.T("report.kudos") + "\n")
	b.WriteString(l.T("report.kudos_summary", "count", k.Count, "people", k.Receivers) + "\n")
	for _, h := range k.Highlights {
		fmt.Fprintf(&b, "%s\n-# %s → %s · %s\n", quote(truncate(h.Message, kudosMax)),
			h.FromUsername, h.ToUsername, dayLabel(l, h.Day))
	}
	if more := k.Count - len(k.Highlights); more > 0 {
		b.WriteString("-# " + l.T("report.more_kudos", "n", more) + "\n")
	}
	return b.String()
}

// dayLabel is "Fri 23 Oct" in the locale's names.
func dayLabel(l *i18n.Locale, day string) string {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return fmt.Sprintf("%s %d %s", l.List("date.weekdays")[d.Weekday()], d.Day(), l.List("date.months")[d.Month()-1])
}

// shortDate is "23 oct" in the locale's names.
func shortDate(l *i18n.Locale, day string) string {
	d, err := time.Parse(isoDate, day)
	if err != nil {
		return day
	}
	return l.T("date.short", "day", d.Day(), "month", l.List("date.months")[d.Month()-1])
}

// moodColor goes from red (bad week) to green (good week); grey without answers.
func moodColor(r api.Report) int {
	switch {
	case r.Responses == 0:
		return 0x8b8d98
	case r.Average < 2:
		return 0xe5484d
	case r.Average < 3:
		return 0xf5a524
	case r.Average < 4:
		return 0x2a78d6
	default:
		return 0x30a46c
	}
}

func textSummary(l *i18n.Locale, r api.Report, o reportOptions) string {
	if r.Responses == 0 {
		return l.T("report.no_responses") + "\n\n"
	}
	var b strings.Builder
	b.WriteString(summary(l, r, o))
	b.WriteString("\n\n")
	for i := len(moodEmojis); i >= 1; i-- {
		n := r.Distribution[fmt.Sprint(i)]
		fmt.Fprintf(&b, "%s %s %d\n", moodEmoji(i), bar(n, r.Counted()), n)
	}
	return b.String() + "\n"
}

func bar(n, total int) string {
	const width = 10
	filled := 0
	if total > 0 {
		filled = (n*width + total/2) / total
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
