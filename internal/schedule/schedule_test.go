package schedule

import (
	"testing"
	"time"
)

func TestSurveyDue(t *testing.T) {
	s := Settings{Timezone: "Europe/Madrid", SurveyHour: 13}
	// 11:30 UTC is 13:30 in Madrid (CEST, UTC+2).
	day, due := SurveyDue(s, time.Date(2026, 10, 22, 11, 30, 0, 0, time.UTC))
	if !due || day != "2026-10-22" {
		t.Fatalf("got %s %v, want 2026-10-22 true", day, due)
	}
	if _, due := SurveyDue(s, time.Date(2026, 10, 22, 10, 59, 0, 0, time.UTC)); due {
		t.Fatal("should not be due before the survey hour")
	}
	// 22:30 UTC is already the next day in Madrid.
	if day, _ := SurveyDue(s, time.Date(2026, 10, 22, 22, 30, 0, 0, time.UTC)); day != "2026-10-23" {
		t.Fatalf("day should follow the guild timezone, got %s", day)
	}
	// Saturday is off by default.
	if _, due := SurveyDue(s, time.Date(2026, 10, 24, 11, 30, 0, 0, time.UTC)); due {
		t.Fatal("should not be due on a weekend by default")
	}
	// Sunday to Thursday: due on Sunday, not on Friday.
	s.WorkingDays = []int{7, 1, 2, 3, 4}
	if _, due := SurveyDue(s, time.Date(2026, 10, 25, 12, 30, 0, 0, time.UTC)); !due {
		t.Fatal("should be due on a working Sunday")
	}
	if _, due := SurveyDue(s, time.Date(2026, 10, 23, 11, 30, 0, 0, time.UTC)); due {
		t.Fatal("should not be due on a Friday off")
	}
}

func TestReportDue(t *testing.T) {
	weekly := Settings{Timezone: "UTC", SurveyHour: 9, ReportFrequency: "weekly"}
	from, to, due := ReportDue(weekly, time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)) // Monday
	if !due || from != "2026-10-19" || to != "2026-10-25" {
		t.Fatalf("weekly: got %s..%s %v", from, to, due)
	}
	if _, _, due := ReportDue(weekly, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)); due {
		t.Fatal("weekly report only on Mondays")
	}
	if _, _, due := ReportDue(weekly, time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)); due {
		t.Fatal("not before the survey hour")
	}

	monthly := Settings{Timezone: "UTC", SurveyHour: 9, ReportFrequency: "monthly"}
	from, to, due = ReportDue(monthly, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	if !due || from != "2026-10-01" || to != "2026-10-31" {
		t.Fatalf("monthly: got %s..%s %v", from, to, due)
	}
}

func TestCurrentPeriod(t *testing.T) {
	now := time.Date(2026, 10, 22, 22, 30, 0, 0, time.UTC) // Friday 23rd in Madrid
	weekly := Settings{Timezone: "Europe/Madrid", ReportFrequency: "weekly"}
	if from, to := CurrentPeriod(weekly, now); from != "2026-10-19" || to != "2026-10-23" {
		t.Fatalf("weekly: got %s..%s", from, to)
	}
	monthly := Settings{Timezone: "Europe/Madrid", ReportFrequency: "monthly"}
	if from, to := CurrentPeriod(monthly, now); from != "2026-10-01" || to != "2026-10-23" {
		t.Fatalf("monthly: got %s..%s", from, to)
	}
	// On a Monday the week in progress is just today.
	if from, to := CurrentPeriod(weekly, time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)); from != "2026-10-26" || to != "2026-10-26" {
		t.Fatalf("monday: got %s..%s", from, to)
	}
}

func TestGameDue(t *testing.T) {
	s := Settings{Timezone: "UTC", SurveyHour: 9}
	for day, want := range map[int][2]bool{ // Oct 2026: 19 is a Monday
		19: {true, false}, 21: {true, false}, 22: {false, true}, 25: {false, true}, 26: {true, false},
	} {
		now := time.Date(2026, 10, day, 10, 0, 0, 0, time.UTC)
		qWeek, q := GameQuestionDue(s, now)
		gWeek, g := GameGuessDue(s, now)
		if q != want[0] || g != want[1] {
			t.Errorf("Oct %d: question %v guess %v, want %v", day, q, g, want)
		}
		wantWeek := "2026-10-19"
		if day == 26 {
			wantWeek = "2026-10-26"
		}
		if (q && qWeek != wantWeek) || (g && gWeek != wantWeek) {
			t.Errorf("Oct %d: week %q/%q, want %s", day, qWeek, gWeek, wantWeek)
		}
	}
	if _, due := GameQuestionDue(s, time.Date(2026, 10, 19, 8, 59, 0, 0, time.UTC)); due {
		t.Error("question due before the survey hour")
	}
}

func TestNextSurvey(t *testing.T) {
	s := Settings{Timezone: "Europe/Madrid", SurveyHour: 13}
	madrid, _ := time.LoadLocation("Europe/Madrid")
	cases := []struct{ now, want string }{
		{"2026-11-02 09:00", "2026-11-02 13:00"}, // Monday, before the hour
		{"2026-11-02 13:00", "2026-11-03 13:00"}, // the hour itself has started
		{"2026-11-06 15:00", "2026-11-09 13:00"}, // Friday afternoon skips the weekend
		{"2026-11-07 10:00", "2026-11-09 13:00"}, // Saturday
	}
	for _, c := range cases {
		now, _ := time.ParseInLocation("2006-01-02 15:04", c.now, madrid)
		got, ok := NextSurvey(s, now)
		if !ok || got.Format("2006-01-02 15:04") != c.want {
			t.Errorf("NextSurvey(%s) = %s %v, want %s", c.now, got.Format("2006-01-02 15:04"), ok, c.want)
		}
	}
	if _, ok := NextSurvey(Settings{Timezone: "UTC", WorkingDays: []int{}}, time.Now()); !ok {
		t.Error("empty working days mean Monday to Friday")
	}
}
