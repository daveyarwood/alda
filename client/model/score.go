package model

import (
	"regexp"
	"strconv"

	"alda.io/client/color"
	"alda.io/client/help"
	"alda.io/client/json"
)

// The ScoreUpdate interface is implemented by events that update a score.
type ScoreUpdate interface {
	json.RepresentableAsJSON
	HasSourceContext

	// UpdateScore modifies a score and returns nil, or returns an error if
	// something went wrong.
	UpdateScore(score *Score) error

	// DurationMs returns a number of milliseconds representing how long an
	// event takes. For events where duration is not relevant (e.g. an octave
	// change event), this can return 0.
	//
	// The context for this is the Cram event, which involves summing the
	// duration of a number of events and then time-scaling them into a fixed
	// duration.
	DurationMs(part *Part) float64

	// VariableValue returns the value that is captured when an event is part of
	// a variable definition.
	//
	// Returns an error if the value cannot be captured.
	VariableValue(score *Score) (ScoreUpdate, error)
}

// The ScoreEvent interface is implemented by events that occur at moments of
// time in a score.
type ScoreEvent interface {
	json.RepresentableAsJSON

	// EventOffset returns the offset of the event, represented as a number of
	// milliseconds after the beginning of the score.
	EventOffset() float64
}

// A Score is a data structure representing a musical score.
//
// Scores are built up via events (structs which implement ScoreUpdate) that
// update aspects of the score data.
//
// chordMode: When true, notes/rests added to the score are placed at the same
// offset. Otherwise, they are appended sequentially.
type Score struct {
	Parts            []*Part
	CurrentParts     []*Part
	Aliases          map[string][]*Part
	Events           []ScoreEvent
	GlobalAttributes *GlobalAttributes
	Markers          map[string]float64
	Variables        map[string][]ScoreUpdate
	chordMode        bool
}

// JSON implements RepresentableAsJSON.JSON.
func (score *Score) JSON() *json.Container {
	parts := json.Object()
	for _, part := range score.Parts {
		parts.Set(part.JSON(), part.ID())
	}

	currentParts := json.Array()
	for _, part := range score.CurrentParts {
		currentParts.ArrayAppend(part.ID())
	}

	aliases := json.Object()
	for alias, parts := range score.Aliases {
		partIDs := json.Array()
		for _, part := range parts {
			partIDs.ArrayAppend(part.ID())
		}

		aliases.Set(partIDs, alias)
	}

	events := json.Array()
	for _, event := range score.Events {
		events.ArrayAppend(event.JSON())
	}

	variables := json.Object()
	for name, events := range score.Variables {
		eventsArray := json.Array()
		for _, event := range events {
			eventsArray.ArrayAppend(event.JSON())
		}

		variables.Set(eventsArray, name)
	}

	return json.Object(
		"parts", parts,
		"current-parts", currentParts,
		"aliases", aliases,
		"events", events,
		"global-attributes", score.GlobalAttributes.JSON(),
		"markers", score.Markers,
		"variables", variables,
	)
}

// NewScore returns an initialized score.
func NewScore() *Score {
	return &Score{
		Parts:            []*Part{},
		Aliases:          map[string][]*Part{},
		GlobalAttributes: NewGlobalAttributes(),
		Markers:          map[string]float64{},
		Variables:        map[string][]ScoreUpdate{},
	}
}

// Update applies a variable number of ScoreUpdates to a Score, short-circuiting
// and returning the first error that occurs.
//
// Returns nil if no error occurs.
func (score *Score) Update(updates ...ScoreUpdate) error {
	for _, update := range updates {
		if err := update.UpdateScore(score); err != nil {
			return &AldaSourceError{Context: update.GetSourceContext(), Err: err}
		}
	}

	return nil
}

// Tracks returns a map of Part instances to track numbers for the purposes of
// transmitting score data.
func (score *Score) Tracks() map[*Part]int32 {
	tracks := map[*Part]int32{}

	for i, part := range score.Parts {
		tracks[part.origin] = int32(i + 1)
	}

	return tracks
}

// PartOffsets returns a map of Part instances to their current offsets.
func (score *Score) PartOffsets() map[*Part]float64 {
	offsets := map[*Part]float64{}

	for _, part := range score.Parts {
		offsets[part.origin] = part.origin.CurrentOffset
	}

	return offsets
}

// InterpretOffsetReference interprets a string as a specific offset in the
// score in milliseconds.
//
// Returns an error if the string cannot be interpreted as a reference to a
// particular offset in the score.
//
// Examples of valid offset references include:
// * Time markings, e.g. "0:30"
// * Names of markers that are defined in the score
func (score *Score) InterpretOffsetReference(
	reference string,
) (float64, error) {
	re := regexp.MustCompile(`^(\d+):(\d+)$`)
	captured := re.FindStringSubmatch(reference)
	if len(captured) == 3 {
		// captured[0] is the full string, e.g. "0:10"
		minutes, _ := strconv.Atoi(captured[1])
		seconds, _ := strconv.Atoi(captured[2])
		return (float64)(minutes*60*1000) + (float64)(seconds*1000), nil
	}

	offset, hit := score.Markers[reference]
	if !hit {
		return 0, help.UserFacingErrorf(
			`%s is not a valid offset reference.

Valid offset references include:
  • A minute-and-second time marking (e.g. %s)
  • The name of a marker in the score (e.g. %s)`,
			color.Aurora.BrightYellow(reference),
			color.Aurora.BrightYellow("0:30"),
			color.Aurora.BrightYellow("verse2"),
		)
	}

	return offset, nil
}

// TempoItinerary returns a map of offsets to the tempo value that starts at
// that offset.
//
// In an Alda score, each part has its own tempo and it can differ from the
// other parts' tempos.
//
// Nonetheless, we need to maintain a notion of a single "master" tempo in order
// to support features like MIDI export.
//
// A score has exactly one part whose role is the tempo "master". The "master
// tempo" is derived from local tempo attribute changes for that part, as well
// as global tempo attribute changes.
func (score *Score) TempoItinerary() map[float64]float64 {
	itinerary := map[float64]float64{0: 120}

	for _, part := range score.Parts {
		if part.TempoRole != TempoRoleMaster {
			continue
		}

		for offset, tempo := range part.TempoValues {
			itinerary[offset] = tempo
		}
	}

	// Global metric modulation is kind of awkward to deal with for our purposes
	// here, because the tempo that ends up getting set can vary from part to part
	// if the parts happen to be playing at different tempos. Unlike a global
	// tempo change, it isn't clear what tempo to record in the tempo itinerary.
	//
	// This isn't an amazing solution, but it's the best thing I could come up
	// with, and I think it will work in 99% of use cases where this is likely to
	// come up. In scores where there is a global metric modulation, it's probably
	// pretty safe to assume that there was a preceding global tempo update
	// earlier in the score, and that that's probably the tempo that all of the
	// parts are at at the point in the score when the metric modulation occurs.
	// So, if we're operating under that assumption, then we can just keep track
	// of the last global tempo change that we saw and apply the metric modulation
	// to that tempo, and we'll probably be right. Hopefully.
	lastGlobalTempo := 120.0

	for _, offset := range score.GlobalAttributes.offsets {
		for _, update := range score.GlobalAttributes.itinerary[offset] {
			switch update := update.(type) {
			case TempoSet:
				lastGlobalTempo = update.Tempo
				itinerary[offset] = update.Tempo
			case MetricModulation:
				itinerary[offset] = lastGlobalTempo * update.Ratio
			}
		}
	}

	return itinerary
}
