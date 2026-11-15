// Package i18n holds the bot's translations. Each language is one JSON file in locales/,
// named by its code (en.json, es.json, pt-BR.json…). To add a language, copy en.json, translate
// the values and keep the {placeholders}; the tests check every file has the same keys and
// placeholders as English. Keys starting with "_" are metadata:
//
//	"_name":    the language's own name, shown in /happyx config
//	"_discord": the Discord client locales that map to this language
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Default is used for new guilds and for any key a translation lacks.
const Default = "en"

//go:embed locales/*.json
var files embed.FS

// Locale is one language's catalog.
type Locale struct {
	Code     string
	Name     string
	Discord  []string
	messages map[string]string
	fallback *Locale
}

var (
	locales = map[string]*Locale{}
	codes   []string
)

func init() {
	if err := load(files); err != nil {
		panic(err)
	}
}

func load(fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, "locales")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if path.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := fs.ReadFile(fsys, "locales/"+e.Name())
		if err != nil {
			return err
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		l := &Locale{Code: strings.TrimSuffix(e.Name(), ".json"), messages: map[string]string{}}
		for k, v := range raw {
			switch k {
			case "_name":
				l.Name, _ = v.(string)
			case "_discord":
				for _, d := range v.([]any) {
					l.Discord = append(l.Discord, d.(string))
				}
			default:
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("%s: %q must be a string", e.Name(), k)
				}
				l.messages[k] = s
			}
		}
		locales[l.Code] = l
		codes = append(codes, l.Code)
	}
	en, ok := locales[Default]
	if !ok {
		return fmt.Errorf("missing locales/%s.json", Default)
	}
	for _, l := range locales {
		if l != en {
			l.fallback = en
		}
	}
	sort.Strings(codes)
	return nil
}

// Codes lists the available languages, sorted.
func Codes() []string { return append([]string(nil), codes...) }

// Get returns the catalog for code ("pt-BR" falls back to "pt", then to English).
func Get(code string) *Locale {
	if l, ok := locales[code]; ok {
		return l
	}
	if base, _, ok := strings.Cut(code, "-"); ok {
		if l, ok := locales[base]; ok {
			return l
		}
	}
	return locales[Default]
}

// Supported reports whether code has its own catalog.
func Supported(code string) bool {
	_, ok := locales[code]
	return ok
}

// FromDiscord maps a Discord client locale (e.g. "es-ES") to a language, if one claims it.
func FromDiscord(discordLocale string) (*Locale, bool) {
	for _, l := range locales {
		for _, d := range l.Discord {
			if d == discordLocale {
				return l, true
			}
		}
	}
	return nil, false
}

// T returns the message for key with {placeholders} filled from name/value pairs:
//
//	l.T("survey.question", "guild", g.Name)
func (l *Locale) T(key string, args ...any) string {
	msg, ok := l.messages[key]
	if !ok && l.fallback != nil {
		msg, ok = l.fallback.messages[key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return msg
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		pairs = append(pairs, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}

// Has reports whether this language (not its fallback) defines key.
func (l *Locale) Has(key string) bool {
	_, ok := l.messages[key]
	return ok
}

// Decimal writes v with one decimal and the language's decimal separator: 3.7 or 3,7.
func (l *Locale) Decimal(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", l.T("number.decimal"), 1)
}

// List splits a comma separated message, used for month and weekday names.
func (l *Locale) List(key string) []string {
	return strings.Split(l.T(key), ",")
}
