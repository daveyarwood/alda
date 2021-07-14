package code_generator

import (
	"alda.io/client/color"
	log "alda.io/client/logging"
	"alda.io/client/model"
	"alda.io/client/parser"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type generator struct {
	tokens []parser.Token
}

func (g *generator) addToken(tokenType parser.TokenType, text string) {
	g.tokens = append(g.tokens, parser.Token{TokenType: tokenType, Text: text})
}

func (g *generator) generateAttributeUpdate(au model.AttributeUpdate) {
	switch partUpdate := au.PartUpdate.(type) {
	case model.OctaveSet:
		g.addToken(
			parser.OctaveSet, fmt.Sprintf("o%d", partUpdate.OctaveNumber),
		)
	case model.OctaveUp:
		g.addToken(parser.OctaveUp, ">")
	case model.OctaveDown:
		g.addToken(parser.OctaveDown, "<")
	default:
		// No other AttributeUpdates can be introduced directly into Alda
		g.logError(partUpdate)
	}
}

func (g *generator) logError(structure interface{}) {
	log.Error().Msg(fmt.Sprintf(
		`Alda code generator cannot process struct with type %s`,
		color.Aurora.BrightYellow(reflect.TypeOf(structure).Name()),
	))
}

func (g *generator) generateGlobalAttributeUpdate(
	ga model.GlobalAttributeUpdate,
) {
	// GlobalAttributeUpdates can only be introduced using Lisp
	g.logError(ga)
}

func (g *generator) generateBarline(_ model.Barline) {
	g.addToken(parser.Barline, "|")
}

func (g *generator) generateChord(chord model.Chord) {
	for index, update := range chord.Events {
		g.generateTokens([]model.ScoreUpdate{update})

		if index < len(chord.Events) - 1 {
			g.addToken(parser.Separator, "/")
		}
	}
}

func (g *generator) generateCram(cram model.Cram) {
	g.addToken(parser.CramOpen, "{")
	g.generateTokens(cram.Events)
	g.addToken(parser.CramClose, "}")
}

func (g *generator) generateEventSequence(es model.EventSequence) {
	g.addToken(parser.EventSeqOpen, "[")
	g.generateTokens(es.Events)
	g.addToken(parser.EventSeqClose, "]")
}

func (g *generator) generateLispNil(n model.LispNil) {
	g.logError(n)
}

func (g *generator) generateLispList(l model.LispList) {
	for _, element := range l.Elements {
		switch lispForm := element.(type) {
		case model.LispNumber:
			g.addToken(parser.Number, fmt.Sprintf("%f", lispForm.Value))
		case model.LispString:
			g.addToken(parser.String, lispForm.Value)
		case model.LispSymbol:
			g.addToken(parser.Symbol, lispForm.Name)
		case model.LispSpecialFormQuote:
		case model.LispAny:
		case model.LispVariadic:
		case model.LispFunction:
		case model.LispNil:
		case model.LispQuotedForm:
		case model.LispScoreUpdate:
		case model.LispPitch:
		case model.LispDuration:
		case model.LispList:
		default:
			g.logError(element)
		}
	}
}

func (g *generator) generateMarker(marker model.Marker) {
	g.addToken(parser.Marker, "%" + marker.Name)
}

func (g *generator) generateAtMarker(atMarker model.AtMarker) {
	g.addToken(parser.AtMarker, "@" + atMarker.Name)
}

func (g *generator) generatePitch(pitch model.PitchIdentifier) {
	// PitchIdentifier can be a LetterAndAccidentals or a MidiNoteNumber
	// However, note's are never generated directly with a MidiNoteNumber pitch
	// When parsing Alda itself, MidiNoteNumber is only used with Lisp
	// When building Alda from the MusicXML importer, MidiNoteNumbers are
	// transformed to LetterAndAccidentals during optimization
	if reflect.TypeOf(pitch) == reflect.TypeOf(model.MidiNoteNumber{}) {
		g.logError(pitch)
	}

	laa := pitch.(model.LetterAndAccidentals)
	g.addToken(parser.NoteLetter, strings.ToLower(laa.NoteLetter.String()))

	for _, accidental := range laa.Accidentals {
		switch accidental {
		case model.Flat:
			g.addToken(parser.Flat, "-")
		case model.Natural:
			g.addToken(parser.Natural, "_")
		case model.Sharp:
			g.addToken(parser.Sharp, "+")
		}
	}
}

func (g *generator) generateDuration(duration model.Duration) {
	for _, component := range duration.Components {
		switch value := component.(type) {
		case model.Barline:
			g.generateBarline(value)
		case model.NoteLength:
			noteLength := fmt.Sprintf("%f", value.Denominator) +
				strings.Repeat(".", int(value.Dots))
			g.addToken(parser.NoteLength, noteLength)
		case model.NoteLengthMs:
			noteLengthMs := fmt.Sprintf("%f", value.Quantity) + "ms"
			g.addToken(parser.NoteLengthMs, noteLengthMs)
		case model.NoteLengthBeats:
			// NoteLengthBeats is only generated from Lisp
			g.logError(component)
		}
	}
}

func (g *generator) generateNote(note model.Note) {
	g.generatePitch(note.Pitch)
	g.generateDuration(note.Duration)
	if note.Slurred {
		g.addToken(parser.Tie, "~")
	}
}

func (g *generator) generateRest(rest model.Rest) {
	g.addToken(parser.RestLetter, "r")
	g.generateDuration(rest.Duration)
}

func (g *generator) generatePartDeclaration(decl model.PartDeclaration) {
	for i, name := range decl.Names {
		if i > 0 {
			g.addToken(parser.Separator, "/")
		}
		g.addToken(parser.Name, name)
	}

	if decl.Alias != "" {
		g.addToken(parser.Alias, "\"" + decl.Alias + "\"")
	}

	g.addToken(parser.Colon, ":")
}

func (g *generator) generateRepeat(repeat model.Repeat) {
	g.generateTokens([]model.ScoreUpdate{repeat.Event})
	g.addToken(parser.Repeat, "*" + strconv.Itoa(int(repeat.Times)))
}

func (g *generator) generateOnRepetitions(or model.OnRepetitions) {
	g.generateTokens([]model.ScoreUpdate{or.Event})

	repetitionsText := "'"
	for i, repetition := range or.Repetitions {
		if i > 0 {
			repetitionsText += ","
		}
		if repetition.First == repetition.Last {
			repetitionsText += strconv.Itoa(int(repetition.First))
		} else {
			repetitionsText += strconv.Itoa(int(repetition.First))
			repetitionsText += "-"
			repetitionsText += strconv.Itoa(int(repetition.Last))
		}
	}

	g.addToken(parser.Repetitions, repetitionsText)
}

func (g *generator) generateVariableDeclaration(vd model.VariableDefinition) {
	g.addToken(parser.Name, vd.VariableName)
	g.addToken(parser.Equals, "=")
	g.generateTokens(vd.Events)
}

func (g *generator) generateVariableReference(vr model.VariableReference) {
	g.addToken(parser.Name, vr.VariableName)
}

func (g *generator) generateVoiceMarker(vm model.VoiceMarker) {
	g.addToken(
		parser.VoiceMarker,
		"V" + strconv.Itoa(int(vm.VoiceNumber)) + ":",
	)
}

func (g *generator) generateVoiceGroupEndMarker(_ model.VoiceGroupEndMarker) {
	g.addToken(parser.VoiceMarker, "V0:")
}

func (g *generator) generateTokens(scoreUpdates []model.ScoreUpdate) {
	for _, update := range scoreUpdates {
		switch value := update.(type) {
		case model.AttributeUpdate:
			g.generateAttributeUpdate(value)
		case model.GlobalAttributeUpdate:
			g.generateGlobalAttributeUpdate(value)
		case model.Barline:
			g.generateBarline(value)
		case model.Chord:
			g.generateChord(value)
		case model.Cram:
			g.generateCram(value)
		case model.EventSequence:
			g.generateEventSequence(value)
		case model.LispNil:
			g.generateLispNil(value)
		case model.LispList:
			g.generateLispList(value)
		case model.Marker:
			g.generateMarker(value)
		case model.AtMarker:
			g.generateAtMarker(value)
		case model.Note:
			g.generateNote(value)
		case model.Rest:
			g.generateRest(value)
		case model.PartDeclaration:
			g.generatePartDeclaration(value)
		case model.Repeat:
			g.generateRepeat(value)
		case model.OnRepetitions:
			g.generateOnRepetitions(value)
		case model.VariableDefinition:
			g.generateVariableDeclaration(value)
		case model.VariableReference:
			g.generateVariableReference(value)
		case model.VoiceMarker:
			g.generateVoiceMarker(value)
		case model.VoiceGroupEndMarker:
			g.generateVoiceGroupEndMarker(value)
		}
	}
}

// Generate transforms Alda score updates into tokens for the purpose of
// generating idiomatic Alda as the first step of the Alda decompiler
// Generate does not generate tokens equivalent to parsed tokens, and instead
// drops token literals as they are unnecessary for formatting token text
// Generate operates on a subset of valid Alda, and is meant to be run against
// score updates as imported from external sources such as MusicXML
func Generate(scoreUpdates []model.ScoreUpdate) []parser.Token {
	var g generator
	g.generateTokens(scoreUpdates)
	g.addToken(parser.EOF, "")
	return g.tokens
}
