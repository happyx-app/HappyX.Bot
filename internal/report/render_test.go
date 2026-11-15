package report

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fogleman/gg"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

func sample(days int, skip map[int]bool) api.Report {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	r := api.Report{
		DiscordGuildID: "100000000000000001",
		From:           start.Format(dateFmt),
		To:             start.AddDate(0, 0, days-1).Format(dateFmt),
		Anonymous:      true,
		Distribution:   map[string]int{"1": 1, "2": 3, "3": 8, "4": 12, "5": 5},
		Messages:       []api.ReportMsg{{Day: "2026-10-19", Mood: 4, Message: "bien"}},
		Kudos:          api.KudosSummary{Count: 4, Givers: 3, Receivers: 2},
	}
	total := 0
	for i := 0; i < days; i++ {
		d := api.ReportDay{Day: start.AddDate(0, 0, i).Format(dateFmt)}
		if !skip[i] {
			d.Responses = 4 + i%3
			d.Average = 2.5 + float64(i%5)*0.5
			total += d.Responses
		}
		r.Days = append(r.Days, d)
	}
	r.Responses, r.Respondents, r.Average = 29, 6, 3.6
	return r
}

func TestRender(t *testing.T) {
	cases := map[string]api.Report{
		"weekly":  sample(7, map[int]bool{5: true}),
		"monthly": sample(31, map[int]bool{3: true, 4: true, 10: true}),
		"single":  sample(1, nil),
		"empty":   {From: "2026-10-19", To: "2026-10-25", Anonymous: true, Distribution: map[string]int{}},
	}
	for _, lang := range i18n.Codes() {
		for name, r := range cases {
			name := lang + "-" + name
			t.Run(name, func(t *testing.T) {
				b, err := Render(i18n.Get(lang), "Servidor de prueba con un nombre bastante largo para ver cómo se corta", r)
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(b))
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds().Dx() != width {
					t.Fatalf("width = %d", img.Bounds().Dx())
				}
				// REPORT_PNG_DIR=/some/dir writes the images for a visual check.
				if dir := os.Getenv("REPORT_PNG_DIR"); dir != "" {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("report-%s.png", name)), b, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

// The footer says what the report leaves out; in every language it breaks into lines that fit
// rather than running off the image.
func TestFooterFits(t *testing.T) {
	r := sample(7, nil)
	r.Days[1].Withheld = true
	r.Messages = append(r.Messages, api.ReportMsg{Message: "otro"}, api.ReportMsg{Message: "y otro"})
	dc := gg.NewContext(1, 1)
	setFont(dc, regular, 18)
	for _, lang := range i18n.Codes() {
		lines := footerLines(i18n.Get(lang), r)
		if got := strings.Count(strings.Join(lines, " · "), " · "); got != 2 {
			t.Errorf("%s: footer has %d separators, want 2 (three parts): %q", lang, got, lines)
		}
		for _, line := range lines {
			if w, _ := dc.MeasureString(line); w > width-2*pad {
				t.Errorf("%s: footer line %q is %.0f wide, room is %.0f", lang, line, w, width-2*pad)
			}
		}
	}
}
