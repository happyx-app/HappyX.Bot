// Command preview writes an HTML page that shows the bot's messages the way Discord would, built
// with the bot's real message code and sample data. It needs no Discord server or API:
//
//	go run ./cmd/preview -lang es -out preview.html
package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/happyx-app/HappyX.Bot/internal/bot"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// logo is the bot's avatar, the same drawing the web uses (HappyX.Web src/logo.svg).
//
//go:embed logo.svg
var logo []byte

// names shows the sample IDs as Discord would.
var names = map[string]string{
	bot.PreviewAdminID:   "alex",
	bot.PreviewAnaID:     "ana",
	bot.PreviewBeaID:     "bea",
	bot.PreviewChannelID: "kudos",
	bot.PreviewGeneralID: "general",
}

// images stands in for the sample avatars and server icon with a letter on a colour.
var images = map[string]string{
	bot.PreviewGuildIcon: placeholder("E", "#3b5bdb", 16),
	bot.PreviewAvatarAna: placeholder("A", "#e8590c", 32),
	bot.PreviewAvatarBea: placeholder("B", "#0ca678", 32),
}

func placeholder(letter, colour string, radius int) string {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="%d" fill="%s"/>`+
		`<text x="32" y="43" font-family="sans-serif" font-size="30" font-weight="700" fill="#fff" text-anchor="middle">%s</text></svg>`, radius, colour, letter)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func main() {
	lang := flag.String("lang", i18n.Default, "language code: "+strings.Join(i18n.Codes(), ", "))
	out := flag.String("out", "preview.html", "file to write")
	flag.Parse()

	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "<h1>HappyX · %s</h1>\n", html.EscapeString(i18n.Get(*lang).Name))
	for i, p := range bot.Previews(*lang) {
		fmt.Fprintf(&b, "<section id=\"scene-%d\"><div class=\"scene\">%s <span>%s</span></div>\n",
			i+1, html.EscapeString(p.Title), html.EscapeString(p.Channel))
		if p.Command != "" {
			fmt.Fprintf(&b, "<div class=\"reply-to\">↱ <b>ana</b> used <span class=\"cmd\">%s</span></div>\n", html.EscapeString(p.Command))
		}
		fmt.Fprintf(&b, `<div class="msg"><img class="avatar" alt="" src="data:image/svg+xml;base64,%s"><div class="body">`, base64.StdEncoding.EncodeToString(logo))
		b.WriteString(`<div class="author">HappyX <span class="tag">APP</span> <span class="time">Today at 13:00</span></div>`)
		writeMessage(&b, p.Msg)
		if p.Ephemeral {
			b.WriteString(`<div class="ephemeral">👁 Only you can see this · <u>Dismiss message</u></div>`)
		}
		b.WriteString("</div></div></section>\n")
	}
	b.WriteString("</body></html>\n")
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", *out)
}

func writeMessage(b *strings.Builder, m *discordgo.MessageSend) {
	if m.Content != "" {
		fmt.Fprintf(b, "<div class=\"content\">%s</div>", markdown(m.Content))
	}
	files := map[string]string{}
	for _, f := range m.Files {
		data, _ := io.ReadAll(f.Reader)
		files["attachment://"+f.Name] = "data:" + f.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	for _, e := range m.Embeds {
		fmt.Fprintf(b, "<div class=\"embed\" style=\"border-color:#%06x\">", e.Color)
		if e.Thumbnail != nil {
			fmt.Fprintf(b, "<img class=\"thumb\" alt=\"\" src=\"%s\">", images[e.Thumbnail.URL])
		}
		if e.Author != nil {
			fmt.Fprintf(b, "<div class=\"eauthor\">%s</div>", html.EscapeString(e.Author.Name))
		}
		if e.Title != "" {
			fmt.Fprintf(b, "<div class=\"etitle\">%s</div>", markdown(e.Title))
		}
		if e.Description != "" {
			fmt.Fprintf(b, "<div class=\"edesc\">%s</div>", markdown(e.Description))
		}
		if len(e.Fields) > 0 {
			b.WriteString(`<div class="efields">`)
			for _, f := range e.Fields {
				fmt.Fprintf(b, "<div><div class=\"fname\">%s</div>%s</div>", html.EscapeString(f.Name), markdown(f.Value))
			}
			b.WriteString("</div>")
		}
		if e.Image != nil {
			fmt.Fprintf(b, "<img src=\"%s\">", files[e.Image.URL])
		}
		if e.Footer != nil {
			fmt.Fprintf(b, "<div class=\"efoot\">%s</div>", html.EscapeString(e.Footer.Text))
		}
		b.WriteString("</div>")
	}
	writeComponents(b, m.Components)
}

// writeComponents draws button rows, and the Components v2 layout: containers, text,
// separators and selects (showing what is chosen, or the placeholder).
func writeComponents(b *strings.Builder, components []discordgo.MessageComponent) {
	for _, c := range components {
		switch c := c.(type) {
		case discordgo.Container:
			colour := 0
			if c.AccentColor != nil {
				colour = *c.AccentColor
			}
			fmt.Fprintf(b, "<div class=\"container\" style=\"border-color:#%06x\">", colour)
			writeComponents(b, c.Components)
			b.WriteString("</div>")
		case discordgo.TextDisplay:
			fmt.Fprintf(b, "<div class=\"content\">%s</div>", markdown(c.Content))
		case discordgo.Separator:
			b.WriteString("<hr>")
		case discordgo.ActionsRow:
			b.WriteString(`<div class="row">`)
			for _, rc := range c.Components {
				switch rc := rc.(type) {
				case discordgo.Button:
					writeButton(b, rc)
				case discordgo.SelectMenu:
					var chosen []string
					for _, o := range rc.Options {
						if o.Default {
							chosen = append(chosen, html.EscapeString(o.Label))
						}
					}
					text, class := strings.Join(chosen, ", "), "select"
					if text == "" {
						text, class = html.EscapeString(rc.Placeholder), "select empty"
					}
					fmt.Fprintf(b, "<span class=\"%s\">%s<i>⌄</i></span>", class, text)
				}
			}
			b.WriteString("</div>")
		}
	}
}

func writeButton(b *strings.Builder, btn discordgo.Button) {
	class, label := "secondary", html.EscapeString(btn.Label)
	switch btn.Style {
	case discordgo.PrimaryButton:
		class = "primary"
	case discordgo.SuccessButton:
		class = "success"
	case discordgo.LinkButton:
		label += " ↗"
	}
	emoji := ""
	if btn.Emoji != nil {
		emoji = btn.Emoji.Name + " "
	}
	fmt.Fprintf(b, "<span class=\"btn %s\">%s%s</span>", class, emoji, label)
}

var (
	reMention = regexp.MustCompile(`&lt;@(\d+)&gt;`)
	reChannel = regexp.MustCompile(`&lt;#(\d+)&gt;`)
	reCommand = regexp.MustCompile(`&lt;/([^:&]+):\d+&gt;`)
	reBold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCode    = regexp.MustCompile("`([^`]+)`")
)

// markdown renders the subset of Discord markdown the bot uses.
func markdown(s string) string {
	var out bytes.Buffer
	var quote []string
	flush := func() {
		if len(quote) > 0 {
			fmt.Fprintf(&out, "<blockquote>%s</blockquote>", strings.Join(quote, "<br>"))
			quote = nil
		}
	}
	for _, line := range strings.Split(s, "\n") {
		l := inline(html.EscapeString(line))
		switch {
		case strings.HasPrefix(line, "> "):
			quote = append(quote, inline(html.EscapeString(line[2:])))
			continue
		case strings.HasPrefix(line, "### "):
			flush()
			fmt.Fprintf(&out, "<div class=\"h3\">%s</div>", inline(html.EscapeString(line[4:])))
		case strings.HasPrefix(line, "-# "):
			flush()
			fmt.Fprintf(&out, "<div class=\"sub\">%s</div>", inline(html.EscapeString(line[3:])))
		default:
			flush()
			fmt.Fprintf(&out, "<div>%s</div>", l)
		}
	}
	flush()
	return out.String()
}

func inline(s string) string {
	s = reMention.ReplaceAllStringFunc(s, func(m string) string {
		return "<span class=\"mention\">@" + name(reMention.FindStringSubmatch(m)[1]) + "</span>"
	})
	s = reChannel.ReplaceAllStringFunc(s, func(m string) string {
		return "<span class=\"mention\">#" + name(reChannel.FindStringSubmatch(m)[1]) + "</span>"
	})
	s = reCommand.ReplaceAllString(s, `<span class="mention">/$1</span>`)
	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	return reCode.ReplaceAllString(s, "<code>$1</code>")
}

func name(id string) string {
	if n, ok := names[id]; ok {
		return n
	}
	return id
}

const head = `<!doctype html><html><head><meta charset="utf-8"><title>HappyX preview</title><style>
body{background:#313338;color:#dbdee1;font:16px/1.375 "gg sans","Noto Sans","Helvetica Neue",Arial,sans-serif;margin:0;padding:24px;max-width:760px}
h1{font-size:20px;color:#f2f3f5}
section{padding:16px 0;border-top:1px solid #3f4147}
.scene{color:#949ba4;font-size:12px;text-transform:uppercase;font-weight:700;margin-bottom:8px}.scene span{background:#2b2d31;border-radius:4px;padding:1px 6px;text-transform:none;font-weight:500}
.reply-to{color:#949ba4;font-size:14px;margin-left:56px}.cmd{color:#c9cdfb}
.msg{display:flex;gap:16px}.avatar{width:40px;height:40px;border-radius:50%;flex:none}
.body{min-width:0;flex:1}.author{color:#f2f3f5;font-weight:500}.tag{background:#5865f2;color:#fff;font-size:10px;border-radius:3px;padding:1px 4px;vertical-align:2px}.time{color:#949ba4;font-size:12px;font-weight:400}
.content div{min-height:1em}
blockquote{margin:2px 0;padding:0 0 0 12px;border-left:4px solid #4e5058}
.h3{font-size:16px;font-weight:700;margin:8px 0 2px;color:#f2f3f5}.sub{font-size:13px;color:#949ba4}
.mention{background:rgba(88,101,242,.3);color:#c9cdfb;border-radius:3px;padding:0 2px;font-weight:500}
code{background:#2b2d31;border-radius:4px;padding:0 3px;font-size:14px}
.embed{background:#2b2d31;border-left:4px solid;border-radius:4px;padding:8px 16px 16px 12px;margin-top:4px;max-width:520px;font-size:14px}
.embed{display:grid;grid-template-columns:minmax(0,1fr) auto;column-gap:16px}.embed>*{grid-column:1}.embed img.thumb{grid-column:2;grid-row:1/span 6;width:80px;height:80px;margin:8px 0 0;border-radius:4px}
.eauthor{font-size:14px;font-weight:600;color:#f2f3f5;margin:8px 0 4px}.efields{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin-top:8px}.fname{font-weight:600;color:#f2f3f5;margin-bottom:2px}
.etitle{font-weight:700;color:#f2f3f5;font-size:16px;margin:4px 0 6px}.embed img{max-width:100%;border-radius:4px;margin-top:12px;display:block}.efoot{font-size:12px;color:#949ba4;margin-top:8px}
.row{display:flex;flex-wrap:wrap;gap:8px;margin-top:8px}.btn{border-radius:3px;padding:2px 16px;height:32px;display:inline-flex;align-items:center;font-size:14px;font-weight:500;color:#fff;background:#4e5058}.btn.primary{background:#5865f2}.btn.success{background:#248046}
.container{background:#2b2d31;border-left:4px solid;border-radius:8px;padding:12px 16px 16px;margin-top:4px;max-width:520px;font-size:15px}.container>.content{margin-top:8px}
.container hr{border:0;border-top:1px solid #3f4147;margin:12px 0 4px}
.select{flex:1;display:flex;justify-content:space-between;align-items:center;background:#1e1f22;border:1px solid #1e1f22;border-radius:4px;padding:8px 12px;font-size:14px;color:#dbdee1}.select.empty{color:#949ba4}.select i{font-style:normal;color:#b5bac1}
.ephemeral{font-size:12px;color:#949ba4;margin-top:4px}.ephemeral u{color:#00a8fc;text-decoration:none}
</style></head><body>
`
