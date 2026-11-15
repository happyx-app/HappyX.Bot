package bot

import "testing"

func TestSurveyIDRoundTrip(t *testing.T) {
	id := surveyID{Action: actionMood, GuildID: "100000000000000001", Day: "2026-10-22", Mood: 4}
	got, ok := parseSurveyID(id.String())
	if !ok || got != id {
		t.Fatalf("round trip: got %+v %v", got, ok)
	}
	if len(id.String()) > 100 {
		t.Fatal("custom IDs must fit Discord's 100 character limit")
	}
	stop := surveyID{Action: actionStop, GuildID: "100000000000000001", Day: "2026-10-22"}
	if got, ok := parseSurveyID(stop.String()); !ok || got != stop {
		t.Fatalf("stop round trip: got %+v %v", got, ok)
	}
	opts := surveyID{Action: actionPauseOK, GuildID: "100000000000000001", Day: "2026-10-22", Msg: "1300000000000000001"}
	if got, ok := parseSurveyID(opts.String()); !ok || got != opts || len(opts.String()) > 100 {
		t.Fatalf("options round trip with the survey message: got %+v %v", got, ok)
	}
	for _, bad := range []string{"hx:pause:1:d:0:notanid", "hx:opts:1:d:2", "hx:change:1:d:0", "", "hx:mood:1:2026-10-22", "hx:mood:1:2026-10-22:9", "hx:mood:1:d:0", "hx:stop:1:d:3", "xx:mood:1:d:3", "hx:drop:1:d:3"} {
		if _, ok := parseSurveyID(bad); ok {
			t.Fatalf("%q should not parse", bad)
		}
	}
}

func TestKudosID(t *testing.T) {
	for _, id := range []kudosID{
		{To: "200000000000000002"},
		{To: "200000000000000002", ChannelID: "400000000000000002", MessageID: "500000000000000001"},
	} {
		got, ok := parseKudosID(id.String())
		if !ok || got != id {
			t.Errorf("round trip %+v: %+v %v", id, got, ok)
		}
	}
	opts := surveyID{Action: actionPauseOK, GuildID: "100000000000000001", Day: "2026-10-22", Msg: "1300000000000000001"}
	if got, ok := parseSurveyID(opts.String()); !ok || got != opts || len(opts.String()) > 100 {
		t.Fatalf("options round trip with the survey message: got %+v %v", got, ok)
	}
	for _, bad := range []string{"hx:pause:1:d:0:notanid", "hx:opts:1:d:2", "hx:change:1:d:0", "", "hxk", "hxk:abc::", "hxk:2:3:", "hxk:2::4", "hx:2::", "hxk:2:3:x"} {
		if _, ok := parseKudosID(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
	if _, ok := parseSurveyID(kudosID{To: "2"}.String()); ok {
		t.Error("a kudos ID must not parse as a survey ID")
	}
}
