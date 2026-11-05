package bot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happyx-app/HappyX.Bot/internal/api"
)

// TestTickWaitsForTheSurveyToStart checks a server that hasn't started the survey gets neither
// the survey nor the game's DMs, while its report still goes out and the week's round still
// starts, so the team can play on the web.
func TestTickWaitsForTheSurveyToStart(t *testing.T) {
	off, on := false, true
	for _, c := range []struct {
		name    string
		enabled *bool
		want    []string
		rounds  int // rounds started without a claim, so without DMs
	}{
		{"not started", &off, []string{"report"}, 1},
		{"started", &on, []string{"survey", "report", "game"}, 0},
		{"older API", nil, []string{"survey", "report", "game"}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var claimed []string
			rounds := 0
			g := api.Guild{DiscordID: "100000000000000001", Timezone: "UTC", SurveyHour: 9, ReportFrequency: "weekly",
				WorkingDays: []int{1, 2, 3, 4, 5}, GamesEnabled: true, SurveyEnabled: c.enabled}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/removals":
					io.WriteString(w, `[]`)
				case r.Method == http.MethodGet && r.URL.Path == "/v1/guilds":
					json.NewEncoder(w).Encode([]api.Guild{g})
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dispatches"):
					var in struct{ Kind string }
					json.NewDecoder(r.Body).Decode(&in)
					mu.Lock()
					claimed = append(claimed, in.Kind)
					mu.Unlock()
					w.WriteHeader(http.StatusConflict) // already sent: nothing to do
					io.WriteString(w, `{"error":"already dispatched"}`)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/game/rounds"):
					mu.Lock()
					rounds++
					mu.Unlock()
					io.WriteString(w, `{"week":"2026-11-02","question":"?"}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			monday := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC) // survey, report and game day
			b := &Bot{api: api.New(srv.URL, "key"), log: slog.New(slog.NewTextHandler(io.Discard, nil)),
				now: func() time.Time { return monday }, skip: map[string]time.Time{}}
			b.tick(context.Background())
			b.tick(context.Background()) // a second tick doesn't start the round again
			if strings.Join(claimed, ",") != strings.Join(c.want, ",") {
				t.Fatalf("claimed %v, want %v", claimed, c.want)
			}
			if rounds != c.rounds {
				t.Fatalf("started %d rounds without DMs, want %d", rounds, c.rounds)
			}
		})
	}
}
