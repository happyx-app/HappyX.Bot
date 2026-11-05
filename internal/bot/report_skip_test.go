package bot

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/happyx-app/HappyX.Bot/internal/api"
)

// TestEmptyReportIsNotSent checks the admin gets no report DM for a period without a single
// answer or kudo, such as one where the survey was off. The bot has no Discord session here, so
// trying to DM would panic.
func TestEmptyReportIsNotSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"discord_guild_id":"100000000000000001","from":"2026-10-26","to":"2026-11-01","responses":0,"kudos":{"count":0}}`)
	}))
	defer srv.Close()
	b := &Bot{api: api.New(srv.URL, "key"), log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		now: time.Now, skip: map[string]time.Time{}}
	g := api.Guild{DiscordID: "100000000000000001", AdminDiscordID: "300000000000000001"}
	d, err := b.sendReport(context.Background(), g, "2026-10-26", "2026-11-01")
	if err != nil || d.recipients != 0 {
		t.Fatalf("delivery %+v, err %v", d, err)
	}
}
