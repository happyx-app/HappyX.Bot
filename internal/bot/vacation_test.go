package bot

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

func TestParseVacationDate(t *testing.T) {
	nov6 := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"20/12":      "2026-12-20",
		"6/1":        "2027-01-06", // already past this year: next January
		"6/11":       "2026-11-06", // today
		"2/11":       "2026-11-02", // a vacation that already started
		"1/8":        "2027-08-01", // more than 90 days ago: next year
		"20/12/2026": "2026-12-20",
		"20-12-26":   "2026-12-20",
		"20.12.":     "2026-12-20",
		" 20.12 ":    "2026-12-20",
		"2027-01-06": "2027-01-06",
		"29/2/2028":  "2028-02-29",
		"31/2":       "",
		"13/13":      "",
		"tomorrow":   "",
		"":           "",
	} {
		got, ok := parseVacationDate(in, nov6)
		if want == "" {
			if ok {
				t.Errorf("%q: got %s, want an error", in, got.Format(isoDate))
			}
			continue
		}
		if !ok || got.Format(isoDate) != want {
			t.Errorf("%q: got %s %v, want %s", in, got.Format(isoDate), ok, want)
		}
	}
	// The last day without a year comes after the first one.
	from, _ := parseVacationDate("28/12", nov6)
	if to, _ := parseVacationDate("6/1", from); to.Format(isoDate) != "2027-01-06" {
		t.Fatalf("to after from: %s", to.Format(isoDate))
	}
}

func TestVacationStatus(t *testing.T) {
	l := i18n.Get("es")
	vs := []api.Vacation{{From: "2026-11-09", To: "2026-11-13"}}
	lines, away := vacationStatus(l, vs, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC))
	if away || !strings.Contains(lines, "**Vacaciones:** del <t:") {
		t.Fatalf("upcoming: %q %v", lines, away)
	}
	if _, away := vacationStatus(l, vs, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)); !away {
		t.Fatal("last day not counted as away")
	}
	if lines, away := vacationStatus(l, nil, time.Now()); lines != "" || away {
		t.Fatalf("no vacations: %q", lines)
	}
}

// discordDate must show the same day wherever the reader is.
func TestDiscordDate(t *testing.T) {
	got := discordDate("2026-12-20")
	want := "<t:" + itoa(time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC).Unix()) + ":D>"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestVacationCommand(t *testing.T) {
	for _, c := range commands() {
		if c.Name != "happyx" {
			continue
		}
		for _, o := range c.Options {
			if o.Name == cmdVacation {
				if len(o.Options) != 2 || !o.Options[0].Required || !o.Options[1].Required {
					t.Fatalf("vacation options: %+v", o.Options)
				}
				return
			}
		}
	}
	t.Fatal("no /happyx vacation")
}
