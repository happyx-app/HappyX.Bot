// Package api is a small client for HappyX.Api.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	ErrTooMany   = errors.New("too many requests")
	// ErrGone means the server was deleted from HappyX and the bot must leave it.
	ErrGone = errors.New("gone")
)

// Error is a non-2xx answer from the API.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("api: %d %s", e.Status, e.Message) }

func (e *Error) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusConflict
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrTooMany:
		return e.Status == http.StatusTooManyRequests
	case ErrGone:
		return e.Status == http.StatusGone
	}
	return false
}

type Client struct {
	base string
	key  string
	http *http.Client
}

func New(baseURL, key string) *Client {
	return &Client{base: baseURL, key: key, http: &http.Client{Timeout: 10 * time.Second}}
}

type Guild struct {
	DiscordID       string `json:"discord_guild_id"`
	Name            string `json:"name"`
	AdminDiscordID  string `json:"admin_discord_user_id"`
	Anonymous       bool   `json:"anonymous"`
	Timezone        string `json:"timezone"`
	SurveyHour      int    `json:"survey_hour"`
	ReportFrequency string `json:"report_frequency"`
	// ReportDM is false when the main admin chose to see reports only on the web. Missing (an
	// older API) means true.
	ReportDM *bool `json:"report_dm"`
	// SurveyEnabled is false while the server hasn't started the survey: nobody gets the survey
	// or the game. A server the bot just joined starts with it off. Missing (an older API) means
	// true.
	SurveyEnabled  *bool  `json:"survey_enabled"`
	Language       string `json:"language"`
	KudosChannelID string `json:"kudos_channel_id"` // empty: where the command is used
	GamesEnabled   bool   `json:"games_enabled"`
	WorkingDays    []int  `json:"working_days"` // ISO days the survey goes out, 1 is Monday
	// SurveyMessage is the admin's own survey text with {{placeholders}}; empty means the default.
	SurveyMessage string `json:"survey_message"`
	// SurveyRoles and GameRoles narrow by role who gets the survey and who plays the game.
	SurveyRoles RoleFilter `json:"survey_roles"`
	GameRoles   RoleFilter `json:"game_roles"`
	Active      bool       `json:"active"`
}

// RoleFilter modes: everyone, only members with one of the roles, or everyone but them.
const (
	RolesEveryone = "all"
	RolesOnly     = "only"
	RolesExcept   = "except"
)

// RoleFilter narrows a setting to some Discord roles. RoleIDs is empty with RolesEveryone.
type RoleFilter struct {
	Mode    string   `json:"mode"`
	RoleIDs []string `json:"role_ids"`
}

// Everyone reports whether the filter lets every member in, as an older API's empty one does.
func (f RoleFilter) Everyone() bool { return f.Mode != RolesOnly && f.Mode != RolesExcept }

// SendsReportDM reports whether the main admin gets the report as a DM.
func (g Guild) SendsReportDM() bool { return g.ReportDM == nil || *g.ReportDM }

// SendsSurvey reports whether the server started the survey, so members get it and the game.
func (g Guild) SendsSurvey() bool { return g.SurveyEnabled == nil || *g.SurveyEnabled }

type Member struct {
	DiscordID string `json:"discord_user_id"`
	Username  string `json:"username"`
	Active    bool   `json:"active"`
	// Streak is how many days in a row they answered; only filled by SurveyRecipients withStreak.
	Streak int `json:"streak"`
	// PolicyPending is true while they haven't answered in the guild since the privacy policy's
	// date; only filled by SurveyRecipients.
	PolicyPending bool `json:"policy_pending"`
}

type GuildPatch struct {
	Anonymous       *bool       `json:"anonymous,omitempty"`
	Timezone        *string     `json:"timezone,omitempty"`
	SurveyHour      *int        `json:"survey_hour,omitempty"`
	ReportFrequency *string     `json:"report_frequency,omitempty"`
	AdminDiscordID  *string     `json:"admin_discord_user_id,omitempty"`
	Language        *string     `json:"language,omitempty"`
	KudosChannelID  *string     `json:"kudos_channel_id,omitempty"`
	GamesEnabled    *bool       `json:"games_enabled,omitempty"`
	WorkingDays     []int       `json:"working_days,omitempty"`
	ReportDM        *bool       `json:"report_dm,omitempty"`
	SurveyEnabled   *bool       `json:"survey_enabled,omitempty"`
	SurveyRoles     *RoleFilter `json:"survey_roles,omitempty"`
	GameRoles       *RoleFilter `json:"game_roles,omitempty"`
}

type Report struct {
	DiscordGuildID string         `json:"discord_guild_id"`
	From           string         `json:"from"`
	To             string         `json:"to"`
	Anonymous      bool           `json:"anonymous"`
	Responses      int            `json:"responses"`
	Respondents    int            `json:"respondents"`
	Average        float64        `json:"average"`
	Distribution   map[string]int `json:"distribution"`
	Days           []ReportDay    `json:"days"`
	Messages       []ReportMsg    `json:"messages"`
	Kudos          KudosSummary   `json:"kudos"`
}

// KudosSummary counts the period's kudos; Highlights are the latest few, newest first.
type KudosSummary struct {
	Count      int     `json:"count"`
	Givers     int     `json:"givers"`
	Receivers  int     `json:"receivers"`
	Highlights []Kudos `json:"highlights"`
}

type Kudos struct {
	Day          string `json:"day"`
	FromID       string `json:"from_discord_user_id"`
	FromUsername string `json:"from_username"`
	ToID         string `json:"to_discord_user_id"`
	ToUsername   string `json:"to_username"`
	Message      string `json:"message"`
}

type ReportDay struct {
	Day       string  `json:"day"`
	Responses int     `json:"responses"`
	Average   float64 `json:"average"`
}

type ReportMsg struct {
	Day       string `json:"day"`
	Mood      int    `json:"mood"`
	Message   string `json:"message"`
	DiscordID string `json:"discord_user_id,omitempty"`
	Username  string `json:"username,omitempty"`
}

func (c *Client) RegisterGuild(ctx context.Context, id, name, adminID string) (Guild, error) {
	var g Guild
	err := c.do(ctx, http.MethodPost, "/v1/guilds", map[string]string{
		"discord_guild_id": id, "name": name, "admin_discord_user_id": adminID,
	}, &g)
	return g, err
}

func (c *Client) Guild(ctx context.Context, id string) (Guild, error) {
	var g Guild
	err := c.do(ctx, http.MethodGet, "/v1/guilds/"+url.PathEscape(id), nil, &g)
	return g, err
}

func (c *Client) ActiveGuilds(ctx context.Context) ([]Guild, error) {
	var gs []Guild
	err := c.do(ctx, http.MethodGet, "/v1/guilds", nil, &gs)
	return gs, err
}

func (c *Client) UpdateGuild(ctx context.Context, id string, p GuildPatch) (Guild, error) {
	var g Guild
	err := c.do(ctx, http.MethodPatch, "/v1/guilds/"+url.PathEscape(id), p, &g)
	return g, err
}

func (c *Client) RemoveGuild(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/guilds/"+url.PathEscape(id), nil, nil)
}

// Removals lists the servers deleted from HappyX on the dashboard that the bot must leave.
func (c *Client) Removals(ctx context.Context) ([]string, error) {
	var ids []string
	err := c.do(ctx, http.MethodGet, "/v1/removals", nil, &ids)
	return ids, err
}

// DoneRemoving tells the API the bot has left a deleted server.
func (c *Client) DoneRemoving(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/removals/"+url.PathEscape(id), nil, nil)
}

// UpsertMember registers or renames a member. active nil keeps an existing member's
// opt-in state; true opts them in.
func (c *Client) UpsertMember(ctx context.Context, guildID, userID, username string, active *bool) error {
	body := map[string]any{"username": username}
	if active != nil {
		body["active"] = *active
	}
	return c.do(ctx, http.MethodPut, memberPath(guildID, userID), body, nil)
}

// SyncMember registers the member or refreshes their username and Discord roles, keeping their
// opt-in state.
func (c *Client) SyncMember(ctx context.Context, guildID, userID, username string, roleIDs []string) error {
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return c.do(ctx, http.MethodPut, memberPath(guildID, userID), map[string]any{"username": username, "role_ids": roleIDs}, nil)
}

// MemberSync is one member in a bulk SyncMembers call.
type MemberSync struct {
	ID       string   `json:"discord_user_id"`
	Username string   `json:"username"`
	RoleIDs  []string `json:"role_ids"`
}

// SyncMembers registers up to 1000 members or refreshes their usernames and roles in one
// request, keeping everyone's opt-in state.
func (c *Client) SyncMembers(ctx context.Context, guildID string, ms []MemberSync) error {
	for i := range ms {
		if ms[i].RoleIDs == nil {
			ms[i].RoleIDs = []string{}
		}
	}
	return c.do(ctx, http.MethodPut, "/v1/guilds/"+url.PathEscape(guildID)+"/members", map[string]any{"members": ms}, nil)
}

func (c *Client) RemoveMember(ctx context.Context, guildID, userID string) error {
	return c.do(ctx, http.MethodDelete, memberPath(guildID, userID), nil, nil)
}

// MemberLeft tells the API the member left the server, which also ends their dashboard access.
func (c *Client) MemberLeft(ctx context.Context, guildID, userID string) error {
	return c.do(ctx, http.MethodDelete, memberPath(guildID, userID)+"?left=true", nil, nil)
}

type Role struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    int    `json:"color"`
	Position int    `json:"position"`
}

// SyncRoles replaces the server's roles, which the dashboard offers when giving access.
func (c *Client) SyncRoles(ctx context.Context, guildID string, roles []Role) error {
	if roles == nil {
		roles = []Role{}
	}
	return c.do(ctx, http.MethodPut, "/v1/guilds/"+url.PathEscape(guildID)+"/roles", map[string]any{"roles": roles}, nil)
}

type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int    `json:"position"` // order in Discord's channel list
	CanPost  bool   `json:"can_post"`
}

// SyncChannels replaces the server's text and announcement channels, which the dashboard offers
// for the kudos channel.
func (c *Client) SyncChannels(ctx context.Context, guildID string, channels []Channel) error {
	if channels == nil {
		channels = []Channel{}
	}
	return c.do(ctx, http.MethodPut, "/v1/guilds/"+url.PathEscape(guildID)+"/channels", map[string]any{"channels": channels}, nil)
}

// Access levels, from least to most.
const (
	AccessNone   = "none"
	AccessView   = "view"   // reports
	AccessManage = "manage" // reports and settings
	AccessOwner  = "owner"  // also who has access
)

// Access returns the member's HappyX level in the server. discordAdmin is true when Discord
// says they own the server or are an Administrator.
func (c *Client) Access(ctx context.Context, guildID, userID string, discordAdmin bool) (string, error) {
	var out struct {
		Level string `json:"level"`
	}
	path := memberPath(guildID, userID) + "/access"
	if discordAdmin {
		path += "?discord_admin=true"
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Level, err
}

func (c *Client) Members(ctx context.Context, guildID string) ([]Member, error) {
	var ms []Member
	err := c.do(ctx, http.MethodGet, "/v1/guilds/"+url.PathEscape(guildID)+"/members", nil, &ms)
	return ms, err
}

// SurveyRecipients is Members without those the server's survey roles leave out of the daily
// survey. withStreak adds each member's answering streak, for {{user.streak}}.
func (c *Client) SurveyRecipients(ctx context.Context, guildID string, withStreak bool) ([]Member, error) {
	path := "/v1/guilds/" + url.PathEscape(guildID) + "/members?for=survey"
	if withStreak {
		path += "&with=streak"
	}
	var ms []Member
	err := c.do(ctx, http.MethodGet, path, nil, &ms)
	return ms, err
}

// GamePlayers is Members without those the server's game roles leave out of «Who's who?».
func (c *Client) GamePlayers(ctx context.Context, guildID string) ([]Member, error) {
	var ms []Member
	err := c.do(ctx, http.MethodGet, "/v1/guilds/"+url.PathEscape(guildID)+"/members?for=game", nil, &ms)
	return ms, err
}

func (c *Client) RecordMood(ctx context.Context, guildID, userID, day string, mood int, message string) error {
	return c.do(ctx, http.MethodPut, memberPath(guildID, userID)+"/mood", map[string]any{
		"mood": mood, "message": message, "day": day,
	}, nil)
}

// GiveKudos records kudos from one member to another. It fails with ErrForbidden when either
// isn't a known member and ErrTooMany when the giver reached today's limit.
func (c *Client) GiveKudos(ctx context.Context, guildID, fromID, toID, message string) (Kudos, error) {
	var k Kudos
	err := c.do(ctx, http.MethodPost, "/v1/guilds/"+url.PathEscape(guildID)+"/kudos", map[string]string{
		"from_discord_user_id": fromID, "to_discord_user_id": toID, "message": message,
	}, &k)
	return k, err
}

// ClaimDispatch returns true when this caller should send the survey or report for period,
// false when it was already sent.
func (c *Client) ClaimDispatch(ctx context.Context, guildID, kind, period string) (bool, error) {
	err := c.do(ctx, http.MethodPost, "/v1/guilds/"+url.PathEscape(guildID)+"/dispatches",
		map[string]string{"kind": kind, "period": period}, nil)
	if errors.Is(err, ErrConflict) {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Message == "already dispatched" {
			return false, nil
		}
	}
	return err == nil, err
}

// FinishDispatch tells the API how many people a claimed send went to and how many DMs failed.
func (c *Client) FinishDispatch(ctx context.Context, guildID, kind, period string, recipients, failed int) error {
	return c.do(ctx, http.MethodPut, "/v1/guilds/"+url.PathEscape(guildID)+"/dispatches/"+url.PathEscape(kind)+"/"+url.PathEscape(period),
		map[string]int{"recipients": recipients, "failed": failed}, nil)
}

// ReleaseDispatch gives back a claimed send that reached nobody, so a later claim can retry it.
func (c *Client) ReleaseDispatch(ctx context.Context, guildID, kind, period string) error {
	return c.do(ctx, http.MethodDelete, "/v1/guilds/"+url.PathEscape(guildID)+"/dispatches/"+url.PathEscape(kind)+"/"+url.PathEscape(period), nil, nil)
}

// Report returns the report for [from, to]; empty strings mean the last complete period.
func (c *Client) Report(ctx context.Context, guildID, from, to string) (Report, error) {
	path := "/v1/guilds/" + url.PathEscape(guildID) + "/report"
	if from != "" || to != "" {
		path += "?" + url.Values{"from": {from}, "to": {to}}.Encode()
	}
	var r Report
	err := c.do(ctx, http.MethodGet, path, nil, &r)
	return r, err
}

// GameRound is a week of the «Who's who?» game.
type GameRound struct {
	Week           string `json:"week"`
	Question       string `json:"question"`
	Phase          string `json:"phase"` // answering, guessing or finished
	GuessingStarts string `json:"guessing_starts"`
	LastDay        string `json:"last_day"`
	Answers        int    `json:"answers"`
	MinAnswers     int    `json:"min_answers"`
	// Proposed is set when the question was drawn from the members' proposals.
	Proposed bool `json:"proposed"`
}

// API messages for the game's 409s.
const (
	MsgGameClosed = "not open now"
	MsgGamesOff   = "games are turned off in this guild"
)

// StartGameRound opens the week's round (a Monday) with question, or returns the round that
// already exists. It fails with ErrConflict when games are off or answering is over.
func (c *Client) StartGameRound(ctx context.Context, guildID, week, question string) (GameRound, error) {
	var r GameRound
	err := c.do(ctx, http.MethodPost, "/v1/guilds/"+url.PathEscape(guildID)+"/game/rounds",
		map[string]string{"week": week, "question": question}, &r)
	return r, err
}

func (c *Client) GameRound(ctx context.Context, guildID, week string) (GameRound, error) {
	var r GameRound
	err := c.do(ctx, http.MethodGet, gameRoundPath(guildID, week), nil, &r)
	return r, err
}

// AnswerGame stores a member's answer. It fails with ErrForbidden for paused members and
// ErrConflict once answers are closed or games are off.
func (c *Client) AnswerGame(ctx context.Context, guildID, week, userID, answer string) error {
	return c.do(ctx, http.MethodPut, gameRoundPath(guildID, week)+"/answers/"+url.PathEscape(userID),
		map[string]string{"answer": answer}, nil)
}

// ProposeQuestion proposes userID's question for next Monday's draw, replacing an earlier one.
// It fails with ErrForbidden for paused members and ErrConflict from Monday to Wednesday or
// when games are off.
func (c *Client) ProposeQuestion(ctx context.Context, guildID, userID, question string) error {
	return c.do(ctx, http.MethodPut, "/v1/guilds/"+url.PathEscape(guildID)+"/game/proposals/"+url.PathEscape(userID),
		map[string]string{"question": question}, nil)
}

func gameRoundPath(guildID, week string) string {
	return "/v1/guilds/" + url.PathEscape(guildID) + "/game/rounds/" + url.PathEscape(week)
}

func memberPath(guildID, userID string) string {
	return "/v1/guilds/" + url.PathEscape(guildID) + "/members/" + url.PathEscape(userID)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&e)
		return &Error{Status: res.StatusCode, Message: e.Error}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}
