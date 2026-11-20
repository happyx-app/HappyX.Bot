package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"
)

// Contract test against a running HappyX.Api. Set HAPPYX_API_URL and HAPPYX_API_KEY to run it.
// It writes to the guild 100000000000000077, so don't point it at production.
func TestClientAgainstAPI(t *testing.T) {
	url, key := os.Getenv("HAPPYX_API_URL"), os.Getenv("HAPPYX_API_KEY")
	if url == "" || key == "" {
		t.Skip("HAPPYX_API_URL / HAPPYX_API_KEY not set")
	}
	ctx := context.Background()
	c := New(url, key)
	const g, admin, u = "100000000000000077", "300000000000000077", "200000000000000077"
	today := time.Now().In(mustLoc(t, "Europe/Madrid")).Format("2006-01-02")

	if _, err := c.RegisterGuild(ctx, g, "Contract test", admin); err != nil {
		t.Fatal(err)
	}
	if err := c.UpsertMember(ctx, g, u, "tester", "Tester", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RecordMood(ctx, g, u, today, 4, "hola"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RecordMood(ctx, g, "200000000000000078", today, 4, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member answer: want ErrForbidden, got %v", err)
	}
	r, err := c.Report(ctx, g, today, today)
	if err != nil || r.Responses < 1 {
		t.Fatalf("report: %+v %v", r, err)
	}

	period := "2001-01-01" // fixed so a rerun sees it already claimed
	first, err := c.ClaimDispatch(ctx, g, "survey", period)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.ClaimDispatch(ctx, g, "survey", period)
	if err != nil || second {
		t.Fatalf("second claim should be false without error: %v %v (first %v)", second, err, first)
	}
	if err := c.FinishDispatch(ctx, g, "survey", period, 3, 1, 1); err != nil {
		t.Fatalf("finish dispatch: %v", err)
	}
	if err := c.FinishDispatch(ctx, g, "survey", "2001-01-02", 3, 1, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finish an unclaimed dispatch: want ErrNotFound, got %v", err)
	}
	if err := c.ReleaseDispatch(ctx, g, "survey", "2001-01-02"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("release an unclaimed dispatch: want ErrNotFound, got %v", err)
	}

	const u2 = "200000000000000079"
	if err := c.UpsertMember(ctx, g, u2, "tester2", "", nil); err != nil {
		t.Fatal(err)
	}
	k, err := c.GiveKudos(ctx, g, u, u2, "gracias")
	if err != nil && !errors.Is(err, ErrTooMany) { // reruns on the same day hit the limit
		t.Fatal(err)
	}
	if err == nil && (k.FromUsername != "tester" || k.ToUsername != "tester2" || k.FromName() != "Tester" || k.ToName() != "tester2") {
		t.Fatalf("kudos should come back with usernames: %+v", k)
	}
	var apiErr *Error
	if _, err := c.GiveKudos(ctx, g, u, u, "yo"); !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("self kudos: want 400, got %v", err)
	}
	if r, err := c.Report(ctx, g, today, today); err != nil || r.Kudos.Count < 1 || len(r.Kudos.Highlights) == 0 {
		t.Fatalf("report kudos: %+v %v", r.Kudos, err)
	}

	if err := c.RemoveMember(ctx, g, u); err != nil {
		t.Fatal(err)
	}
	if err := c.UpsertMember(ctx, g, u, "tester", "Tester", nil); err != nil {
		t.Fatal(err)
	}
	ms, err := c.Members(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.DiscordID == u {
			t.Fatal("sync re-subscribed a member who opted out")
		}
	}

	if err := c.SyncRoles(ctx, g, []Role{{ID: "400000000000000077", Name: "Staff", Color: 0x3498db, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SyncMembers(ctx, g, []MemberSync{{ID: u, Username: "tester", DisplayName: "Tester"}, {ID: u2, Username: "tester2", RoleIDs: []string{"400000000000000077"}}}); err != nil {
		t.Fatal(err)
	}
	if ms, err := c.Members(ctx, g); err != nil {
		t.Fatal(err)
	} else {
		for _, m := range ms {
			if m.DiscordID == u {
				t.Fatal("bulk sync re-subscribed a member who opted out")
			}
		}
	}
	if err := c.SyncMember(ctx, g, u2, "tester2", "", []string{"400000000000000077"}); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[string]string{admin: AccessOwner, u2: AccessNone} {
		if level, err := c.Access(ctx, g, user, false); err != nil || level != want {
			t.Fatalf("access of %s: %q %v, want %q", user, level, err, want)
		}
	}
	if level, err := c.Access(ctx, g, u2, true); err != nil || level != AccessOwner {
		t.Fatalf("Discord administrator: %q %v", level, err)
	}
	// With the survey going only to Staff, the admin (no roles) is left out of it but not of Members.
	setSurveyRoles := func(body map[string]any) {
		t.Helper()
		if err := c.do(ctx, http.MethodPatch, "/v1/guilds/"+g, map[string]any{"survey_roles": body}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.UpsertMember(ctx, g, admin, "admin", "Admin", nil); err != nil {
		t.Fatal(err)
	}
	setSurveyRoles(map[string]any{"mode": "only", "role_ids": []string{"400000000000000077"}})
	if rs, err := c.SurveyRecipients(ctx, g, true); err != nil || len(rs) != 1 || rs[0].DiscordID != u2 {
		t.Fatalf("survey recipients with only Staff: %+v %v", rs, err)
	}
	setSurveyRoles(map[string]any{"mode": "all"})
	// The game has its own list: leaving Staff out of it leaves only the admin to play.
	if err := c.do(ctx, http.MethodPatch, "/v1/guilds/"+g, map[string]any{"game_roles": map[string]any{"mode": "except", "role_ids": []string{"400000000000000077"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if ps, err := c.GamePlayers(ctx, g); err != nil || len(ps) != 1 || ps[0].DiscordID != admin {
		t.Fatalf("game players without Staff: %+v %v", ps, err)
	}
	if err := c.do(ctx, http.MethodPatch, "/v1/guilds/"+g, map[string]any{"game_roles": map[string]any{"mode": "all"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.MemberLeft(ctx, g, u2); err != nil {
		t.Fatal(err)
	}

	ids, err := c.Removals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == g {
			t.Fatal("a registered guild shouldn't be waiting for removal")
		}
	}
	if err := c.DoneRemoving(ctx, g); err != nil {
		t.Fatalf("confirming a removal that isn't queued should be harmless: %v", err)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
