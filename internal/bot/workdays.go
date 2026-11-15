package bot

import (
	"strings"
	"time"

	"github.com/happyx-app/HappyX.Bot/internal/i18n"
)

// workingDayPresets are the choices of /happyx config working_days, as ISO days (1 is
// Monday, 7 Sunday). Any other set can be picked on the web.
var workingDayPresets = []struct {
	key  string
	days []int
}{
	{"mon_fri", []int{1, 2, 3, 4, 5}},
	{"mon_sat", []int{1, 2, 3, 4, 5, 6}},
	{"all", []int{1, 2, 3, 4, 5, 6, 7}},
	{"sun_thu", []int{7, 1, 2, 3, 4}},
}

func presetDays(key string) []int {
	for _, p := range workingDayPresets {
		if p.key == key {
			return p.days
		}
	}
	return nil
}

// workingDaysText writes ISO days the short way, e.g. "Mon–Fri" or "Sun–Thu, Sat". Empty means
// Monday to Friday, for an API that doesn't send them yet.
func workingDaysText(l *i18n.Locale, iso []int) string {
	if len(iso) == 0 {
		iso = presetDays("mon_fri")
	}
	var on [7]bool // index 0 is Monday
	n := 0
	for _, d := range iso {
		if d >= 1 && d <= 7 && !on[d-1] {
			on[d-1] = true
			n++
		}
	}
	if n == 7 {
		return l.T("days.every")
	}
	names := l.List("date.weekdays") // Sunday first
	name := func(i int) string { return names[time.Weekday((i+1)%7)] }

	// Start on Monday, or earlier when a run goes over the weekend into Monday (Sun–Thu).
	start := 0
	for n > 0 && on[0] && on[(start+6)%7] {
		start = (start + 6) % 7
	}
	var parts []string
	for i := 0; i < 7; {
		if !on[(start+i)%7] {
			i++
			continue
		}
		j := i
		for j+1 < 7 && on[(start+j+1)%7] {
			j++
		}
		first, last := name((start+i)%7), name((start+j)%7)
		switch j - i {
		case 0:
			parts = append(parts, first)
		case 1:
			parts = append(parts, first, last)
		default:
			parts = append(parts, first+"–"+last)
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

// workingWeek is the survey days of the week day falls in, in order. A week starts on Monday,
// or earlier when the working days run over the weekend into Monday (Sunday to Thursday starts
// on Sunday), as workingDaysText writes them.
func workingWeek(iso []int, day time.Time) []time.Time {
	if len(iso) == 0 {
		iso = presetDays("mon_fri")
	}
	var on [7]bool // index 0 is Monday
	n := 0
	for _, d := range iso {
		if d >= 1 && d <= 7 && !on[d-1] {
			on[d-1] = true
			n++
		}
	}
	start := 0
	for n > 0 && n < 7 && on[0] && on[(start+6)%7] {
		start = (start + 6) % 7
	}
	idx := (int(day.Weekday()) + 6) % 7 // 0 is Monday
	first := day.AddDate(0, 0, -((idx - start + 7) % 7))
	var out []time.Time
	for i := 0; i < 7; i++ {
		d := first.AddDate(0, 0, i)
		if on[(int(d.Weekday())+6)%7] {
			out = append(out, d)
		}
	}
	return out
}

// lastWorkingDay reports whether day is the last survey day of its week.
func lastWorkingDay(iso []int, day time.Time) bool {
	w := workingWeek(iso, day)
	return len(w) > 0 && w[len(w)-1].Equal(day)
}
