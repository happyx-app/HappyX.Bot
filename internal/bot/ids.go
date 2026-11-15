package bot

import (
	"fmt"
	"strconv"
	"strings"
)

// Survey components live in DMs, which carry no guild, so the guild and day travel in the
// component's custom ID. The user always comes from the interaction itself, and the API checks
// they are a member of that guild, so a forged ID can't write into someone else's data.

const (
	actionMood      = "mood"      // button: record the mood
	actionNote      = "note"      // button: open the comment modal
	actionNoteModal = "notemodal" // modal submit: record mood + comment
	actionChange    = "change"    // button on the answer: show the faces again (Mood is the current one)
	actionRemood    = "remood"    // button on the faces shown by «change»: record the new mood
	actionStop      = "stop"      // button on surveys sent before «Options»: stop getting this guild's survey
	actionOptions   = "opts"      // button: the member's options for this guild's survey
	actionVacation  = "vac"       // button: open the vacation modal
	actionVacModal  = "vacmodal"  // modal submit: set the vacation
	actionPause     = "pause"     // button: ask to confirm the pause
	actionPauseOK   = "pauseok"   // button: pause this guild's survey
	actionCancel    = "cancel"    // button: back to the options
	actionResume    = "resume"    // button on a paused survey: get it again
	idPrefix        = "hx"
)

// surveyID is the custom ID of the survey's components. Msg, when set, is the survey message
// that an options button acts on: the options are an ephemeral reply, a message of their own.
type surveyID struct {
	Action  string
	GuildID string
	Day     string
	Mood    int
	Msg     string
}

func (s surveyID) String() string {
	id := fmt.Sprintf("%s:%s:%s:%s:%d", idPrefix, s.Action, s.GuildID, s.Day, s.Mood)
	if s.Msg != "" {
		id += ":" + s.Msg
	}
	return id
}

func parseSurveyID(v string) (surveyID, bool) {
	p := strings.Split(v, ":")
	if (len(p) != 5 && len(p) != 6) || p[0] != idPrefix {
		return surveyID{}, false
	}
	mood, err := strconv.Atoi(p[4])
	if err != nil {
		return surveyID{}, false
	}
	switch p[1] {
	case actionMood, actionRemood, actionNote, actionNoteModal, actionChange:
		if mood < 1 || mood > 5 {
			return surveyID{}, false
		}
	case actionStop, actionOptions, actionVacation, actionVacModal, actionPause, actionPauseOK, actionCancel, actionResume:
		if mood != 0 {
			return surveyID{}, false
		}
	default:
		return surveyID{}, false
	}
	id := surveyID{Action: p[1], GuildID: p[2], Day: p[3], Mood: mood}
	if len(p) == 6 {
		if !snowflake(p[5]) {
			return surveyID{}, false
		}
		id.Msg = p[5]
	}
	return id, true
}

// kudosID is the custom ID of the modal opened by "Give kudos" in the right-click menu. It
// carries the receiver and, when given from a message, where that message is. The giver and
// the guild come from the interaction itself.
type kudosID struct {
	To        string
	ChannelID string // empty unless given from a message
	MessageID string
}

const kudosPrefix = "hxk"

func (k kudosID) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", kudosPrefix, k.To, k.ChannelID, k.MessageID)
}

func parseKudosID(v string) (kudosID, bool) {
	p := strings.Split(v, ":")
	if len(p) != 4 || p[0] != kudosPrefix || !snowflake(p[1]) {
		return kudosID{}, false
	}
	if (p[2] == "") != (p[3] == "") || (p[2] != "" && (!snowflake(p[2]) || !snowflake(p[3]))) {
		return kudosID{}, false
	}
	return kudosID{To: p[1], ChannelID: p[2], MessageID: p[3]}, true
}

func snowflake(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
