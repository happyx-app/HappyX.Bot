// Package report renders a HappyX report as a PNG image for the guild admin.
package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"github.com/fogleman/gg"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// Palette and type follow the web dashboard (HappyX.Web src/style.css, light theme): cards
// on a warm page, one violet series hue (one measure per chart, so no legend is needed and
// colour carries no identity) and the logo as the only touch of yellow.
var (
	colPage      = hex("#f9f9f7")
	colSurface   = hex("#fcfcfb")
	colBorder    = color.NRGBA{11, 11, 11, 26}
	colText      = hex("#0b0b0b")
	colText2     = hex("#52514e")
	colGrid      = hex("#e1e0d9")
	colAxis      = hex("#c3c2b7")
	colMuted     = hex("#9a9893") // labels of days without answers
	colSeries    = hex("#7c3aed")
	colSeriesDim = hex("#ede9fe")
)

const (
	width   = 1200
	pad     = 48.0
	gap     = 20.0 // between cards
	inset   = 28.0 // inside a card
	radius  = 14.0
	dateFmt = "2006-01-02"
)

// Inter (SIL OFL 1.1, see fonts/OFL.txt), subset to Latin, Greek and Cyrillic.
var (
	//go:embed fonts/Inter-Regular.otf
	interRegular []byte
	//go:embed fonts/Inter-Medium.otf
	interMedium []byte
	//go:embed fonts/Inter-SemiBold.otf
	interSemiBold []byte
	//go:embed fonts/Inter-Bold.otf
	interBold []byte
	//go:embed logo.png
	logoPNG []byte

	regular  = mustFont(interRegular)
	medium   = mustFont(interMedium)
	semibold = mustFont(interSemiBold)
	bold     = mustFont(interBold)
	logo     = mustPNG(logoPNG)
)

// Render draws the report in language l. guildName is shown in the title.
func Render(l *i18n.Locale, guildName string, r api.Report) ([]byte, error) {
	const (
		tilesTop  = 236.0
		tileH     = 160.0
		chartH    = 400.0
		distH     = 2*inset + 40 + 5*rowH + 4*rowGap
		emptyH    = 160.0
		footerGap = 56.0
	)
	chartTop := tilesTop + tileH + gap
	distTop := chartTop + chartH + gap
	bottom := distTop + distH
	if r.Responses == 0 {
		bottom = chartTop + emptyH
	}
	height := int(bottom + footerGap + pad - 8)

	dc := gg.NewContext(width, height)
	dc.SetColor(colPage)
	dc.Clear()

	drawHeader(dc, l, guildName, r)

	// Headline tiles
	avg, avgNote, resp, people := "—", "", "0", "0"
	if r.Responses > 0 {
		avg = l.Decimal(r.Average)
		avgNote = moodLabel(l, int(math.Round(r.Average)))
		resp = fmt.Sprint(r.Responses)
		people = fmt.Sprint(r.Respondents)
	}
	tiles := []struct{ label, value, unit, note string }{
		{l.T("report.average"), avg, "/ 5", avgNote},
		{l.T("report.responses"), resp, "", ""},
		{l.T("report.people"), people, "", ""},
		{l.T("report.kudos"), fmt.Sprint(r.Kudos.Count), "", ""},
	}
	tileW := (width - 2*pad - float64(len(tiles)-1)*gap) / float64(len(tiles))
	for i, t := range tiles {
		x := pad + float64(i)*(tileW+gap)
		drawTile(dc, x, tilesTop, tileW, tileH, t.label, t.value, t.unit, t.note)
	}

	if r.Responses == 0 {
		card(dc, pad, chartTop, width-2*pad, emptyH)
		setFont(dc, medium, 24)
		dc.SetColor(colText2)
		dc.DrawStringAnchored(l.T("report.no_responses"), width/2, chartTop+emptyH/2, 0.5, 0.35)
		drawFooter(dc, l, r, float64(height))
		return encode(dc)
	}

	drawDailyChart(dc, l, r.Days, chartTop, chartH)
	drawDistribution(dc, l, r, distTop, distH)
	drawFooter(dc, l, r, float64(height))
	return encode(dc)
}

// drawHeader is the web top bar (logo + name) followed by the page title and period.
func drawHeader(dc *gg.Context, l *i18n.Locale, guildName string, r api.Report) {
	const logoSize = 40
	dst := image.NewRGBA(image.Rect(0, 0, logoSize, logoSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), logo, logo.Bounds(), draw.Over, nil)
	dc.DrawImage(dst, int(pad), int(pad))
	setFont(dc, semibold, 22)
	dc.SetColor(colText)
	dc.DrawStringAnchored("HappyX", pad+logoSize+12, pad+logoSize/2, 0, 0.35)

	setFont(dc, bold, 40)
	dc.SetColor(colText)
	dc.DrawString(truncate(dc, guildName, width-2*pad), pad, 162)
	setFont(dc, regular, 22)
	dc.SetColor(colText2)
	dc.DrawString(periodLabel(l, r.From, r.To), pad, 200)
}

// card draws the web's card: surface fill with a hairline border.
func card(dc *gg.Context, x, y, w, h float64) {
	dc.DrawRoundedRectangle(x+0.5, y+0.5, w-1, h-1, radius)
	dc.SetColor(colSurface)
	dc.FillPreserve()
	dc.SetColor(colBorder)
	dc.SetLineWidth(1)
	dc.Stroke()
}

func cardTitle(dc *gg.Context, text string, x, y float64) {
	setFont(dc, semibold, 22)
	dc.SetColor(colText)
	dc.DrawString(text, x+inset, y+inset+20)
}

func drawTile(dc *gg.Context, x, y, w, h float64, label, value, unit, note string) {
	card(dc, x, y, w, h)
	setFont(dc, regular, 19)
	dc.SetColor(colText2)
	dc.DrawString(truncate(dc, label, w-2*inset), x+inset, y+inset+18)
	setFont(dc, bold, 52)
	dc.SetColor(colText)
	dc.DrawString(value, x+inset, y+inset+78)
	if unit != "" {
		vw, _ := dc.MeasureString(value)
		setFont(dc, regular, 22)
		dc.SetColor(colText2)
		dc.DrawString(unit, x+inset+vw+8, y+inset+78)
	}
	if note != "" {
		setFont(dc, regular, 18)
		dc.SetColor(colText2)
		dc.DrawString(truncate(dc, note, w-2*inset), x+inset, y+inset+110)
	}
}

// drawDailyChart plots the daily average mood as a line on the 1-5 scale. Days without
// answers break the line.
func drawDailyChart(dc *gg.Context, l *i18n.Locale, days []api.ReportDay, top, h float64) {
	card(dc, pad, top, width-2*pad, h)
	cardTitle(dc, l.T("report.daily"), pad, top)

	labelW := moodLabelWidth(dc, l)
	left, right := pad+inset+labelW, float64(width)-pad-inset
	plotTop, plotBottom := top+inset+64, top+h-inset-44
	yOf := func(v float64) float64 { return plotBottom - (v-1)/4*(plotBottom-plotTop) }

	// Grid and mood labels (recessive); the bottom line is the axis.
	setFont(dc, regular, 18)
	for m := 1; m <= 5; m++ {
		y := yOf(float64(m))
		dc.SetColor(colGrid)
		if m == 1 {
			dc.SetColor(colAxis)
		}
		dc.SetLineWidth(1)
		dc.DrawLine(left, y, right, y)
		dc.Stroke()
		dc.SetColor(colText2)
		dc.DrawStringAnchored(moodLabel(l, m), left-16, y, 1, 0.35)
	}
	if len(days) == 0 {
		return
	}

	step := 0.0
	if len(days) > 1 {
		step = (right - left - 40) / float64(len(days)-1)
	}
	xOf := func(i int) float64 {
		if len(days) == 1 {
			return (left + right) / 2
		}
		return left + 20 + float64(i)*step
	}

	// X labels: every day for a week, about every fifth day for a month.
	every := 1
	if len(days) > 10 {
		every = 5
	}
	setFont(dc, regular, 18)
	for i, d := range days {
		if i%every != 0 && i != len(days)-1 {
			continue
		}
		dc.SetColor(colText2)
		if d.Responses == 0 {
			dc.SetColor(colMuted)
		}
		dc.DrawStringAnchored(dayLabel(l, d.Day, len(days) <= 10), xOf(i), plotBottom+32, 0.5, 0)
	}

	// Line segments between consecutive answered days.
	dc.SetColor(colSeries)
	dc.SetLineWidth(3)
	dc.SetLineCap(gg.LineCapRound)
	dc.SetLineJoin(gg.LineJoinRound)
	drawing := false
	for i, d := range days {
		if d.Responses == 0 {
			if drawing {
				dc.Stroke()
			}
			drawing = false
			continue
		}
		if !drawing {
			dc.MoveTo(xOf(i), yOf(d.Average))
			drawing = true
		} else {
			dc.LineTo(xOf(i), yOf(d.Average))
		}
	}
	if drawing {
		dc.Stroke()
	}

	// Markers with a surface ring so they read over the line. Days without answers get no
	// marker (a dot at the bottom would read as the worst mood); their x label is muted instead.
	for i, d := range days {
		x := xOf(i)
		if d.Responses == 0 {
			continue
		}
		y := yOf(d.Average)
		dc.SetColor(colSurface)
		dc.DrawCircle(x, y, 7.5)
		dc.Fill()
		dc.SetColor(colSeries)
		dc.DrawCircle(x, y, 5.5)
		dc.Fill()
	}
}

const rowH, rowGap = 30.0, 14.0

// drawDistribution shows how many answers each mood got, best first: bars on a soft track,
// count and share on the right like the web's "answers by mood" card.
func drawDistribution(dc *gg.Context, l *i18n.Locale, r api.Report, top, h float64) {
	card(dc, pad, top, width-2*pad, h)
	cardTitle(dc, l.T("report.distribution"), pad, top)

	labelW := moodLabelWidth(dc, l)
	left := pad + inset + labelW
	right := float64(width) - pad - inset
	maxW := right - 120 - left
	maxN := 0
	for m := 1; m <= 5; m++ {
		maxN = max(maxN, r.Distribution[fmt.Sprint(m)])
	}
	for row, m := 0, 5; m >= 1; row, m = row+1, m-1 {
		n := r.Distribution[fmt.Sprint(m)]
		y := top + inset + 44 + float64(row)*(rowH+rowGap)
		setFont(dc, regular, 18)
		dc.SetColor(colText2)
		dc.DrawStringAnchored(moodLabel(l, m), left-16, y+rowH/2, 1, 0.35)

		// Track, then the bar: square at the baseline, rounded at the data end.
		dc.SetColor(colSeriesDim)
		dc.DrawRoundedRectangle(left, y, maxW, rowH, 4)
		dc.Fill()
		w := 0.0
		if maxN > 0 {
			w = math.Round(float64(n) / float64(maxN) * maxW)
		}
		if w > 0 {
			dc.SetColor(colSeries)
			dc.DrawRoundedRectangle(left, y, w, rowH, 4)
			dc.Fill()
			dc.DrawRectangle(left, y, math.Min(4, w), rowH)
			dc.Fill()
		}

		pct := 0
		if r.Responses > 0 {
			pct = int(math.Round(float64(n) * 100 / float64(r.Responses)))
		}
		setFont(dc, medium, 18)
		dc.SetColor(colText)
		dc.DrawStringAnchored(fmt.Sprint(n), right-60, y+rowH/2, 1, 0.35)
		setFont(dc, regular, 18)
		dc.SetColor(colText2)
		dc.DrawStringAnchored(fmt.Sprintf("· %d%%", pct), right-52, y+rowH/2, 0, 0.35)
	}
}

func drawFooter(dc *gg.Context, l *i18n.Locale, r api.Report, height float64) {
	text := l.T("report.anonymous")
	if !r.Anonymous {
		text = l.T("report.named")
	}
	switch n := len(r.Messages); {
	case n == 1:
		text += " · " + l.T("report.one_comment")
	case n > 1:
		text += " · " + l.T("report.n_comments", "n", n)
	}
	setFont(dc, regular, 18)
	dc.SetColor(colText2)
	dc.DrawString(text, pad, height-pad)
}

// --- helpers ---

func moodLabel(l *i18n.Locale, m int) string { return l.T(fmt.Sprintf("mood.%d", m)) }

// moodLabelWidth is the room left of a chart for the mood labels, so long translations fit.
func moodLabelWidth(dc *gg.Context, l *i18n.Locale) float64 {
	setFont(dc, regular, 18)
	w := 0.0
	for m := 1; m <= 5; m++ {
		lw, _ := dc.MeasureString(moodLabel(l, m))
		w = math.Max(w, lw)
	}
	return math.Max(120, w+16)
}

func periodLabel(l *i18n.Locale, from, to string) string {
	f, err1 := time.Parse(dateFmt, from)
	t, err2 := time.Parse(dateFmt, to)
	if err1 != nil || err2 != nil {
		return from + " – " + to
	}
	months := l.List("date.months")
	return l.T("report.image_period",
		"from_day", f.Day(), "from_month", months[f.Month()-1],
		"to_day", t.Day(), "to_month", months[t.Month()-1], "year", t.Year())
}

func dayLabel(l *i18n.Locale, day string, withWeekday bool) string {
	d, err := time.Parse(dateFmt, day)
	if err != nil {
		return day
	}
	if withWeekday {
		return fmt.Sprintf("%s %d", l.List("date.weekdays")[d.Weekday()], d.Day())
	}
	return fmt.Sprint(d.Day())
}

func truncate(dc *gg.Context, s string, maxW float64) string {
	if w, _ := dc.MeasureString(s); w <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if w, _ := dc.MeasureString(string(r) + "…"); w <= maxW {
			return string(r) + "…"
		}
	}
	return ""
}

func encode(dc *gg.Context) ([]byte, error) {
	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mustFont(otf []byte) *opentype.Font {
	f, err := opentype.Parse(otf)
	if err != nil {
		panic(err)
	}
	return f
}

func mustPNG(b []byte) image.Image {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	return img
}

// setFont makes a new face on every call: faces are not safe for concurrent use and
// reports for several guilds can render at the same time.
func setFont(dc *gg.Context, f *opentype.Font, size float64) {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	dc.SetFontFace(face)
}

func hex(s string) color.Color {
	var r, g, b uint8
	fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}
