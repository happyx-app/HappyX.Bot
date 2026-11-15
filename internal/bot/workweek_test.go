package bot

import (
	"testing"
	"time"
)

func TestWorkingWeek(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(isoDate, s); return v }
	days := func(ts []time.Time) string {
		out := ""
		for _, t := range ts {
			out += t.Format("Mon") + " "
		}
		return out
	}
	cases := []struct {
		preset, day, want string
		last              bool
	}{
		{"mon_fri", "2026-10-23", "Mon Tue Wed Thu Fri ", true}, // Friday
		{"mon_fri", "2026-10-22", "Mon Tue Wed Thu Fri ", false},
		{"sun_thu", "2026-10-22", "Sun Mon Tue Wed Thu ", true}, // Thursday, the week from Sunday 18
		{"sun_thu", "2026-10-18", "Sun Mon Tue Wed Thu ", false},
		{"all", "2026-10-25", "Mon Tue Wed Thu Fri Sat Sun ", true},
		{"mon_sat", "2026-10-23", "Mon Tue Wed Thu Fri Sat ", false},
	}
	for _, c := range cases {
		w := workingWeek(presetDays(c.preset), d(c.day))
		if days(w) != c.want || lastWorkingDay(presetDays(c.preset), d(c.day)) != c.last {
			t.Errorf("%s %s: %q last=%v, want %q last=%v", c.preset, c.day, days(w), lastWorkingDay(presetDays(c.preset), d(c.day)), c.want, c.last)
		}
	}
	if w := workingWeek(presetDays("sun_thu"), d("2026-10-22")); !w[0].Equal(d("2026-10-18")) {
		t.Errorf("sun_thu starts on %v", w[0])
	}
	// Monday, Wednesday and Friday: a week from Monday, and only Friday ends it.
	mwf := []int{1, 3, 5}
	if lastWorkingDay(mwf, d("2026-10-19")) || !lastWorkingDay(mwf, d("2026-10-23")) {
		t.Error("Mon/Wed/Fri: only Friday is the last")
	}
}
