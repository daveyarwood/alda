package parser

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"alda.io/client/color"
	"alda.io/client/help"
	log "alda.io/client/logging"
	model "alda.io/client/model"
)

type parser struct {
	filename string
	input    []Token
	updates  []model.ScoreUpdate
	current  int
	// When true, source context is _not_ included in parsed tokens. This is
	// useful for testing, e.g. for checking the equality of a list of expected
	// tokens, agnostic of source context like line and column numbers.
	suppressSourceContext bool
}

// A parseOption is a function that customizes a parser instance.
type parseOption func(*parser)

// SuppressSourceContext customizes a parser to ignore source context
func SuppressSourceContext(parser *parser) {
	parser.suppressSourceContext = true
}

func (p *parser) sourceContext(token Token) model.AldaSourceContext {
	if p.suppressSourceContext {
		return model.AldaSourceContext{}
	}

	return token.SourceContext
}

func newParser(filename string, tokens []Token, opts ...parseOption) *parser {
	parser := &parser{
		filename: filename,
		input:    tokens,
		updates:  []model.ScoreUpdate{},
		current:  0,
	}

	for _, opt := range opts {
		opt(parser)
	}

	return parser
}

////////////////////////////////////////////////////////////////////////////////

func (p *parser) peek() Token {
	return p.input[p.current]
}

func (p *parser) previous() Token {
	return p.input[p.current-1]
}

func (p *parser) check(types ...TokenType) bool {
	for _, tokenType := range types {
		if p.peek().TokenType == tokenType {
			return true
		}
	}

	return false
}

func (p *parser) advance() Token {
	if p.peek().TokenType != EOF {
		p.current++
	}

	return p.previous()
}

func (p *parser) match(types ...TokenType) (Token, bool) {
	for _, tokenType := range types {
		if p.check(tokenType) {
			return p.advance(), true
		}
	}

	return Token{}, false
}

func (p *parser) addUpdate(update model.ScoreUpdate) {
	log.Debug().Str("update", fmt.Sprintf("%#v", update)).Msg("Adding update.")
	p.updates = append(p.updates, update)
}

func (p *parser) errorAtToken(token Token, msg string) *model.AldaSourceError {
	return &model.AldaSourceError{
		Context: token.SourceContext,
		Err:     fmt.Errorf("%s", msg),
	}
}

func (p *parser) unexpectedTokenError(
	token Token, context string,
) *model.AldaSourceError {
	if context != "" {
		context = " " + context
	}

	msg := fmt.Sprintf(
		"Unexpected %s `%s`%s", token.TokenType.String(), token.Text, context,
	)

	return p.errorAtToken(token, msg)
}

func (p *parser) consume(tokenType TokenType, context string) (Token, error) {
	if p.check(tokenType) {
		return p.advance(), nil
	}

	return Token{}, p.unexpectedTokenError(p.peek(), context)
}

func (p *parser) lispForm(context string) (model.LispForm, error) {
	if token, matched := p.match(Symbol); matched {
		return model.LispSymbol{
			SourceContext: p.sourceContext(token),
			Name:          token.Text,
		}, nil
	}

	if token, matched := p.match(Number); matched {
		return model.LispNumber{
			SourceContext: p.sourceContext(token),
			Value:         token.Literal.(float64),
		}, nil
	}

	if token, matched := p.match(String); matched {
		return model.LispString{
			SourceContext: p.sourceContext(token),
			Value:         token.Literal.(string),
		}, nil
	}

	if _, matched := p.match(LeftParen); matched {
		return p.lispList()
	}

	return nil, p.unexpectedTokenError(p.peek(), context)
}

func (p *parser) lispList() (model.LispList, error) {
	// NB: This assumes the initial LeftParen token was already consumed.
	list := model.LispList{SourceContext: p.sourceContext(p.previous())}

	for token := p.peek(); token.TokenType != RightParen; token = p.peek() {
		if _, matched := p.match(EOF); matched {
			return list, p.errorAtToken(token, "Unterminated S-expression.")
		}

		quoteToken, quoted := p.match(SingleQuote)

		form, err := p.lispForm("in S-expression")
		if err != nil {
			return list, err
		}

		if quoted {
			form = model.LispQuotedForm{
				SourceContext: p.sourceContext(quoteToken),
				Form:          form,
			}
		}

		list.Elements = append(list.Elements, form)
	}

	if _, err := p.consume(RightParen, "in S-expression"); err != nil {
		return list, err
	}

	return list, nil
}

func (p *parser) sexp() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial LeftParen token was already consumed.
	list, err := p.lispList()
	if err != nil {
		return nil, err
	}

	return []model.ScoreUpdate{p.singleOrRepeated(list)}, nil
}

func (p *parser) part() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial Name token was already consumed.
	nameToken := p.previous()

	partDecl := model.PartDeclaration{
		SourceContext: p.sourceContext(nameToken),
		Names:         []string{nameToken.Text},
	}

	for {
		if _, matched := p.match(Separator); !matched {
			break
		}

		name, err := p.consume(Name, "in part declaration")
		if err != nil {
			return nil, err
		}

		partDecl.Names = append(partDecl.Names, name.Text)
	}

	if _, matched := p.match(Alias); matched {
		partDecl.Alias = p.previous().Literal.(string)
	}

	if _, err := p.consume(Colon, "in part declaration"); err != nil {
		return nil, err
	}

	return []model.ScoreUpdate{partDecl}, nil
}

func (p *parser) variableDefinition() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial Name token was already consumed.
	nameToken := p.previous()

	definition := model.VariableDefinition{
		SourceContext: p.sourceContext(nameToken),
		VariableName:  nameToken.Text,
	}
	definitionLine := nameToken.SourceContext.Line

	if _, err := p.consume(Equals, "in variable definition"); err != nil {
		return nil, err
	}

	if p.peek().TokenType == EOF || p.peek().SourceContext.Line > definitionLine {
		return nil, &model.AldaSourceError{
			Context: nameToken.SourceContext,
			Err: fmt.Errorf(
				"there must be at least one event on the same line as the '='",
			),
		}
	}

	for t := p.peek(); t.SourceContext.Line == definitionLine; t = p.peek() {
		if t.TokenType == EOF {
			break
		}

		events, err := p.topLevel()
		if err != nil {
			return nil, err
		}
		definition.Events = append(definition.Events, events...)
	}

	return []model.ScoreUpdate{definition}, nil
}

func (p *parser) singleOrRepeated(update model.ScoreUpdate) model.ScoreUpdate {
	if token, matched := p.match(Repeat); matched {
		return model.Repeat{
			SourceContext: p.sourceContext(token),
			Event:         update,
			Times:         token.Literal.(int32),
		}
	}

	return update
}

func (p *parser) variableReference() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial Name token was already consumed.
	nameToken := p.previous()

	reference := model.VariableReference{
		SourceContext: p.sourceContext(nameToken),
		VariableName:  nameToken.Text,
	}

	return []model.ScoreUpdate{p.singleOrRepeated(reference)}, nil
}

func (p *parser) partOrVariableOp() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial Name token was already consumed.
	switch p.peek().TokenType {
	case Equals:
		return p.variableDefinition()
	case Alias, Separator, Colon:
		return p.part()
	default:
		return p.variableReference()
	}
}

func (p *parser) octaveSet() ([]model.ScoreUpdate, error) {
	// NB: This assumes the OctaveSet token was already consumed.
	token := p.previous()

	return []model.ScoreUpdate{
		model.AttributeUpdate{
			SourceContext: p.sourceContext(token),
			PartUpdate:    model.OctaveSet{OctaveNumber: token.Literal.(int32)},
		},
	}, nil
}

func (p *parser) matchDurationComponent() (Token, bool) {
	return p.match(NoteLength, NoteLengthMs)
}

func (p *parser) durationComponent() model.DurationComponent {
	// NB: This assumes the duration component token was already consumed.
	token := p.previous()

	switch token.TokenType {
	case NoteLength:
		nl := token.Literal.(noteLength)
		return model.NoteLength{
			Denominator: nl.denominator,
			Dots:        nl.dots,
		}
	case NoteLengthMs:
		return model.NoteLengthMs{Quantity: token.Literal.(float64)}
	}

	// We shouldn't get here.
	return nil
}

func (p *parser) duration() model.Duration {
	// NB: This assumes the initial duration component token was already consumed.
	duration := model.Duration{
		Components: []model.DurationComponent{p.durationComponent()},
	}

	// Repeatedly parse duration components.
	for {
		// Repeatedly parse barlines amongst the duration components.
		for {
			if token, matched := p.match(Barline); matched {
				duration.Components = append(
					duration.Components,
					model.Barline{SourceContext: p.sourceContext(token)},
				)
			} else {
				break
			}
		}

		// Take note of the current position. If we encounter a tie that ends up
		// actually being a slur (i.e. it isn't followed by a note length), we can
		// backtrack to this position, return the duration, and let the parser
		// consume the slur as part of e.g. a note.
		beforeTies := p.current

		if _, matched := p.match(Tie); !matched {
			return duration
		}

		// We'll stash any barlines that we encounter here temporarily. We'll add
		// them to the duration components iff we aren't going to backtrack and
		// consume them outside of the duration.
		barlines := []model.DurationComponent{}

		for {
			if token, matched := p.match(Barline); matched {
				barlines = append(
					barlines,
					model.Barline{SourceContext: p.sourceContext(token)},
				)
			} else {
				break
			}
		}

		// In some cases, it makes sense to have extraneous ties, e.g. when you're
		// tying a duration across a barline and it feels right to have a tie on
		// either side of the barline. So we'll consume any additional ties here.
		for {
			if _, matched := p.match(Tie); !matched {
				break
			}
		}

		if _, matched := p.matchDurationComponent(); !matched {
			p.current = beforeTies
			return duration
		}

		duration.Components = append(duration.Components, barlines...)
		duration.Components = append(duration.Components, p.durationComponent())
	}
}

func (p *parser) note() (model.Note, error) {
	// NB: This assumes the initial NoteLetter token was already consumed.
	token := p.previous()

	noteLetter, err := model.NewNoteLetter(token.Literal.(rune))
	if err != nil {
		return model.Note{}, err
	}

	pitch := model.LetterAndAccidentals{NoteLetter: noteLetter}

AccidentalsLoop:
	for {
		if _, matched := p.match(Flat); matched {
			pitch.Accidentals = append(pitch.Accidentals, model.Flat)
		} else if _, matched := p.match(Natural); matched {
			pitch.Accidentals = append(pitch.Accidentals, model.Natural)
		} else if _, matched := p.match(Sharp); matched {
			pitch.Accidentals = append(pitch.Accidentals, model.Sharp)
		} else {
			break AccidentalsLoop
		}
	}

	note := model.Note{
		SourceContext: p.sourceContext(token),
		Pitch:         pitch,
	}

	if _, matched := p.matchDurationComponent(); matched {
		note.Duration = p.duration()
	}

	if _, matched := p.match(Tie); matched {
		note.Slurred = true
	}

	return note, nil
}

func (p *parser) rest() model.Rest {
	// NB: This assumes the initial RestLetter token was already consumed.
	token := p.previous()

	rest := model.Rest{SourceContext: p.sourceContext(token)}

	if _, matched := p.matchDurationComponent(); matched {
		rest.Duration = p.duration()
	}

	return rest
}

func (p *parser) noteOrRest() (model.ScoreUpdate, error) {
	// NB: This assumes the initial NoteLetter/RestLetter was already consumed.
	switch letter := p.previous(); letter.TokenType {
	case NoteLetter:
		return p.note()
	case RestLetter:
		return p.rest(), nil
	default:
		return nil, p.unexpectedTokenError(letter, "in note/rest")
	}
}

func (p *parser) updatesBetweenNotesInChord() ([]model.ScoreUpdate, error) {
	updates := []model.ScoreUpdate{}

	for {
		if token, matched := p.match(OctaveUp); matched {
			updates = append(updates, model.AttributeUpdate{
				SourceContext: p.sourceContext(token),
				PartUpdate:    model.OctaveUp{},
			})
		} else if token, matched := p.match(OctaveDown); matched {
			updates = append(updates, model.AttributeUpdate{
				SourceContext: p.sourceContext(token),
				PartUpdate:    model.OctaveDown{},
			})
		} else if _, matched := p.match(OctaveSet); matched {
			octaveSetUpdates, err := p.octaveSet()
			if err != nil {
				return nil, err
			}
			updates = append(updates, octaveSetUpdates...)
		} else if _, matched := p.match(LeftParen); matched {
			sexp, err := p.lispList()
			if err != nil {
				return nil, err
			}
			updates = append(updates, sexp)
		} else {
			return updates, nil
		}
	}
}

// Parses a note or chord. A chord contains multiple chords and rests, not to
// mention attribute changes, so any of those will be parsed too in the process.
func (p *parser) noteRestOrChord() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial NoteLetter/RestLetter was already consumed.

	// The cumulative list of updates. Depending on whether this is a chord, the
	// updates will either be emitted as part of the chord, or emitted
	// individually.
	allUpdates := []model.ScoreUpdate{}

	// We're essentially using this as a nil value. Below, we check whether
	// `repeat.Times` > 0, which will be true if we don't reassign `repeat`.
	repeat := model.Repeat{}

	for {
		noteOrRest, err := p.noteOrRest()
		if err != nil {
			return nil, err
		}

		if token, matched := p.match(Repeat); matched {
			allUpdates = append(allUpdates, noteOrRest)
			repeat = model.Repeat{
				SourceContext: p.sourceContext(token),
				Times:         token.Literal.(int32),
			}
			break
		}

		// The updates for just this repetition of the loop
		updates := []model.ScoreUpdate{noteOrRest}

		updatesBeforeSeparator, err := p.updatesBetweenNotesInChord()
		if err != nil {
			return nil, err
		}
		updates = append(updates, updatesBeforeSeparator...)

		if _, matched := p.match(Separator); !matched {
			allUpdates = append(allUpdates, updates...)
			break
		}

		updatesAfterSeparator, err := p.updatesBetweenNotesInChord()
		if err != nil {
			return nil, err
		}
		updates = append(updates, updatesAfterSeparator...)

		allUpdates = append(allUpdates, updates...)

		if _, matched := p.match(NoteLetter, RestLetter); !matched {
			return nil, p.unexpectedTokenError(p.peek(), "in chord")
		}
	}

	notesCount := 0
	for _, update := range allUpdates {
		switch update.(type) {
		case model.Note, model.Rest:
			notesCount++
		}
	}

	if notesCount > 1 {
		allUpdates = []model.ScoreUpdate{
			model.Chord{
				SourceContext: allUpdates[0].GetSourceContext(),
				Events:        allUpdates,
			},
		}
	}

	if repeat.Times > 0 {
		if len(allUpdates) != 1 {
			panic(fmt.Sprintf("Expected a single update in %#v", allUpdates))
		}

		repeat.Event = allUpdates[0]

		return []model.ScoreUpdate{repeat}, nil
	}

	return allUpdates, nil
}

func (p *parser) repetitions() ([]model.RepetitionRange, error) {
	// NB: This assumes the Repetitions token was already consumed.
	token := p.previous()

	repetitions := []model.RepetitionRange{}

	for _, er := range token.Literal.([]repetitionRange) {
		repetitionRange := model.RepetitionRange{First: er.first, Last: er.last}
		repetitions = append(repetitions, repetitionRange)
	}

	return repetitions, nil
}

func (p *parser) eventSeq() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial EventSeqOpen token was already consumed.
	eventSeqOpenToken := p.previous()

	allEvents := []model.ScoreUpdate{}

	for token := p.peek(); token.TokenType != EventSeqClose; token = p.peek() {
		if _, matched := p.match(EOF); matched {
			return nil, p.errorAtToken(token, "Unterminated event sequence.")
		}

		events, err := p.topLevel()
		if err != nil {
			return nil, err
		}

		if token, matched := p.match(Repetitions); matched {
			repetitions, err := p.repetitions()
			if err != nil {
				return nil, err
			}

			lastI := len(events) - 1
			events[lastI] = model.OnRepetitions{
				SourceContext: p.sourceContext(token),
				Repetitions:   repetitions,
				Event:         events[lastI],
			}
		}

		allEvents = append(allEvents, events...)
	}

	if _, err := p.consume(EventSeqClose, "in event sequence"); err != nil {
		return nil, err
	}

	eventSeq := model.EventSequence{
		SourceContext: p.sourceContext(eventSeqOpenToken),
		Events:        allEvents,
	}

	return []model.ScoreUpdate{p.singleOrRepeated(eventSeq)}, nil
}

func (p *parser) cram() ([]model.ScoreUpdate, error) {
	// NB: This assumes the initial CramOpen token was already consumed.
	cramOpenToken := p.previous()

	allEvents := []model.ScoreUpdate{}

	for token := p.peek(); token.TokenType != CramClose; token = p.peek() {
		if _, matched := p.match(EOF); matched {
			return nil, p.errorAtToken(token, "Unterminated cram expression.")
		}

		events, err := p.topLevel()
		if err != nil {
			return nil, err
		}
		allEvents = append(allEvents, events...)
	}

	if _, err := p.consume(CramClose, "in cram expression"); err != nil {
		return nil, err
	}

	cram := model.Cram{
		SourceContext: p.sourceContext(cramOpenToken),
		Events:        allEvents,
	}

	if _, matched := p.matchDurationComponent(); matched {
		cram.Duration = p.duration()
	}

	return []model.ScoreUpdate{p.singleOrRepeated(cram)}, nil
}

func (p *parser) voiceMarker() ([]model.ScoreUpdate, error) {
	// NB: This assumes the VoiceMarker token was already consumed.
	token := p.previous()

	voiceNumber := token.Literal.(int32)

	if voiceNumber == 0 {
		return []model.ScoreUpdate{
			model.VoiceGroupEndMarker{SourceContext: p.sourceContext(token)},
		}, nil
	}

	return []model.ScoreUpdate{
		model.VoiceMarker{
			SourceContext: p.sourceContext(token),
			VoiceNumber:   voiceNumber,
		},
	}, nil
}

func (p *parser) topLevel() ([]model.ScoreUpdate, error) {
	if _, matched := p.match(LeftParen); matched {
		return p.sexp()
	}

	if _, matched := p.match(Name); matched {
		return p.partOrVariableOp()
	}

	if _, matched := p.match(OctaveSet); matched {
		return p.octaveSet()
	}

	if token, matched := p.match(OctaveUp); matched {
		return []model.ScoreUpdate{
			model.AttributeUpdate{
				SourceContext: p.sourceContext(token),
				PartUpdate:    model.OctaveUp{},
			},
		}, nil
	}

	if token, matched := p.match(OctaveDown); matched {
		return []model.ScoreUpdate{
			model.AttributeUpdate{
				SourceContext: p.sourceContext(token),
				PartUpdate:    model.OctaveDown{},
			},
		}, nil
	}

	if _, matched := p.match(NoteLetter, RestLetter); matched {
		return p.noteRestOrChord()
	}

	if token, matched := p.match(Barline); matched {
		return []model.ScoreUpdate{
			model.Barline{SourceContext: p.sourceContext(token)},
		}, nil
	}

	if _, matched := p.match(EventSeqOpen); matched {
		return p.eventSeq()
	}

	if _, matched := p.match(CramOpen); matched {
		return p.cram()
	}

	if _, matched := p.match(VoiceMarker); matched {
		return p.voiceMarker()
	}

	if token, matched := p.match(Marker); matched {
		return []model.ScoreUpdate{
			model.Marker{
				SourceContext: p.sourceContext(token),
				Name:          token.Literal.(string),
			},
		}, nil
	}

	if token, matched := p.match(AtMarker); matched {
		return []model.ScoreUpdate{
			model.AtMarker{
				SourceContext: p.sourceContext(token),
				Name:          token.Literal.(string),
			},
		}, nil
	}

	return nil, p.unexpectedTokenError(p.peek(), "at the top level")
}

// Parse a string of input into a sequence of score updates.
func Parse(
	filepath string, input string, opts ...parseOption,
) ([]model.ScoreUpdate, error) {
	defer func(start time.Time) {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("Critical error while parsing %s", filepath))
		}

		log.Info().
			Str("filepath", filepath).
			Str("took", fmt.Sprintf("%s", time.Since(start))).
			Msg("Parsed input.")
	}(time.Now())

	tokens, err := Scan(filepath, input)
	if err != nil {
		return nil, err
	}

	p := newParser(filepath, tokens, opts...)

	for t := p.peek(); t.TokenType != EOF; t = p.peek() {
		// log.Debug().Str("token", t.String()).Msg("Parsing token.")

		updates, err := p.topLevel()
		if err != nil {
			return nil, err
		}

		for _, update := range updates {
			p.addUpdate(update)
		}
	}

	return p.updates, nil
}

// ParseString reads and parses a string of input.
func ParseString(input string) ([]model.ScoreUpdate, error) {
	return Parse("", input)
}

// ParseFile reads a file and parses the input.
func ParseFile(filepath string) ([]model.ScoreUpdate, error) {
	contents, err := ioutil.ReadFile(filepath)

	if errors.Is(err, os.ErrNotExist) {
		return nil, help.UserFacingErrorf(
			`Failed to open %s. The file does not seem to exist.

Please check that you haven't misspelled the file name, etc.`,
			color.Aurora.BrightYellow(filepath),
		)
	}

	if err != nil {
		return nil, err
	}

	return Parse(filepath, string(contents))
}
