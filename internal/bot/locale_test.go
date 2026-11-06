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

// TestAdoptLocale checks a server the bot just joined gets the language of its Discord locale,
// unless HappyX lacks it or the server already has another language.
func TestAdoptLocale(t *testing.T) {
	cases := []struct {
		locale, current, want string
	}{
		{"es-ES", "en", "es"},
		{"pt-BR", "en", "pt-BR"},
		{"en-US", "en", ""},
		{"ja", "en", ""},
		{"es-ES", "fr", ""},
	}
	for _, c := range cases {
		got := ""
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			if r.Method == http.MethodPatch {
				got = string(b)
			}
			io.WriteString(w, `{}`)
		}))
		b := &Bot{api: api.New(srv.URL, "key"), log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now}
		b.adoptLocale(context.Background(), api.Guild{DiscordID: "100000000000000001", Language: c.current}, c.locale)
		srv.Close()
		want := ""
		if c.want != "" {
			want = `{"language":"` + c.want + `"}`
		}
		if got != want && got != want+"\n" {
			t.Errorf("%s with %s: sent %q, want %q", c.locale, c.current, got, want)
		}
	}
}
