package parser

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"unicode"

	log "alda.io/client/logging"
	model "alda.io/client/model"
)

func isDigit(c rune) bool {
	return '0' <= c && c <= '9'
}

func isLetter(c rune) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func (s *scanner) errorAtPosition(
	line int, column int, msg string,
) *model.AldaSourceError {
	return &model.AldaSourceError{
		Context: model.AldaSourceContext{
			Filename: s.filename,
			Line:     line,
			Column:   column,
		},
		Err: fmt.Errorf("%s", msg),
	}
}

func (s *scanner) unexpectedCharError(
	c rune, context string, line int, column int) *model.AldaSourceError {
	if context != "" {
		context = " " + context
	}

	var charStr string
	switch {
	case c == 9:
		charStr = "tab"
	case c == 10:
		charStr = "newline"
	case c == 13:
		charStr = "carriage return"
	case unicode.IsControl(c):
		charStr = fmt.Sprintf("control character (%d)", c)
	default:
		charStr = fmt.Sprintf("'%c'", c)
	}

	msg := fmt.Sprintf("Unexpected %s%s", charStr, context)
	return s.errorAtPosition(line, column, msg)
}

// A TokenType is a type of token output by the scanner.
type TokenType int

const (
	Alias TokenType = iota
	AtMarker
	Barline
	Colon
	CramClose
	CramOpen
	EOF
	Equals
	EventSeqClose
	EventSeqOpen
	Flat
	Integer
	LeftParen
	Marker
	Name
	Natural
	NoteLength
	NoteLengthMs
	NoteLetter
	Number
	OctaveDown
	OctaveSet
	OctaveUp
	Repetitions
	RestLetter
	RightParen
	Separator
	Sharp
	SingleQuote
	String
	Symbol
	Tie
	Repeat
	VoiceMarker
)

// A Token is a result of lexical analysis done by the scanner.
type Token struct {
	SourceContext model.AldaSourceContext
	TokenType     TokenType
	Text          string
	Literal       interface{}
}

func (tt TokenType) String() string {
	switch tt {
	case Alias:
		return "alias"
	case AtMarker:
		return "at-marker"
	case Barline:
		return "barline"
	case Colon:
		return "colon"
	case CramClose:
		return "end of cram expression"
	case CramOpen:
		return "start of cram expression"
	case EOF:
		return "EOF"
	case Equals:
		return "equals sign"
	case EventSeqClose:
		return "end of event sequence"
	case EventSeqOpen:
		return "start of event sequence"
	case Flat:
		return "flat"
	case Integer:
		return "integer"
	case LeftParen:
		return "open parenthesis"
	case Marker:
		return "marker"
	case Name:
		return "name"
	case Natural:
		return "natural"
	case NoteLength:
		return "note length"
	case NoteLengthMs:
		return "note length (ms)"
	case NoteLetter:
		return "note letter"
	case Number:
		return "number"
	case OctaveDown:
		return "octave down instruction"
	case OctaveSet:
		return "octave instruction"
	case OctaveUp:
		return "octave up instruction"
	case Repeat:
		return "repeat operator"
	case Repetitions:
		return "repetitions"
	case RestLetter:
		return "rest indicator"
	case RightParen:
		return "close parenthesis"
	case Separator:
		return "separator"
	case Sharp:
		return "sharp"
	case SingleQuote:
		return "single quote"
	case String:
		return "string"
	case Symbol:
		return "symbol"
	case Tie:
		return "tie"
	case VoiceMarker:
		return "voice marker"
	default:
		return fmt.Sprintf("%d (String not implemented)", tt)
	}
}

func (t Token) String() string {
	return fmt.Sprintf(
		"[%d:%d] %s | %#q | %#v",
		t.SourceContext.Line,
		t.SourceContext.Column,
		t.TokenType.String(),
		t.Text,
		t.Literal,
	)
}

type scanner struct {
	filename    string
	input       []rune
	tokens      []Token
	start       int
	current     int
	line        int
	column      int
	startLine   int
	startColumn int
	sexpLevel   int
}

func newScanner(filename string, input string) *scanner {
	return &scanner{
		filename:  filename,
		input:     []rune(input),
		tokens:    []Token{},
		start:     0,
		current:   0,
		line:      1,
		column:    1,
		sexpLevel: 0,
	}
}

func (s *scanner) reachedEOF() bool {
	return s.current >= len(s.input)
}

func (s *scanner) eofIsNext() bool {
	return s.current+1 >= len(s.input)
}

func (s *scanner) peek() rune {
	if s.reachedEOF() {
		return 0
	}

	return s.input[s.current]
}

func (s *scanner) peekNext() rune {
	if s.eofIsNext() {
		return 0
	}

	return s.input[s.current+1]
}

func (s *scanner) advance() rune {
	r := s.input[s.current]
	s.current++

	if r == '\n' {
		s.line++
		s.column = 1
	} else {
		s.column++
	}

	return r
}

func (s *scanner) match(expected rune) bool {
	if s.reachedEOF() {
		return false
	}

	if s.input[s.current] != expected {
		return false
	}

	s.advance()

	return true
}

func (s *scanner) addToken(tokenType TokenType, literal interface{}) {
	text := string(s.input[s.start:s.current])

	token := Token{
		TokenType: tokenType,
		Text:      text,
		Literal:   literal,
		SourceContext: model.AldaSourceContext{
			Filename: s.filename,
			Line:     s.startLine,
			Column:   s.startColumn,
		},
	}

	log.Debug().Str("token", token.String()).Msg("Adding token.")
	s.tokens = append(s.tokens, token)
}

func (s *scanner) skipComment() {
	for s.peek() != '\n' && !s.reachedEOF() {
		s.advance()
	}
}

func (s *scanner) parseString() error {
	// NB: This assumes the initial quote was already consumed.

	for s.peek() != '"' && !s.reachedEOF() {
		s.advance()
	}

	if s.reachedEOF() {
		return s.errorAtPosition(s.line, s.column, "Unterminated string")
	}

	// Consume the closing ".
	s.advance()

	// Trim the surrounding quotes.
	contents := s.input[s.start+1 : s.current-1]
	s.addToken(String, string(contents))

	return nil
}

func (s *scanner) consumeWhile(pred func(rune) bool) {
	for c := s.peek(); !s.reachedEOF() && pred(c); c = s.peek() {
		s.advance()
	}
}

func (s *scanner) consumeDigits() {
	s.consumeWhile(isDigit)
}

func (s *scanner) consumeSpaces() {
	s.consumeWhile(func(c rune) bool { return c == ' ' })
}

// This function is meant to be called after consuming a bunch of digits. It
// reads from the start index up until the current index and parses the result
// as an integer.
func (s *scanner) parseIntegerFrom(startIndex int) int32 {
	integer, _ := strconv.ParseInt(string(s.input[startIndex:s.current]), 10, 32)
	return int32(integer)
}

// This function is meant to be called after consuming a bunch of digits, then
// optionally a period and a bunch more digits. It reads from the start index up
// until the current index and parses the result as a float.
func (s *scanner) parseFloatFrom(startIndex int) float64 {
	number, _ := strconv.ParseFloat(string(s.input[s.start:s.current]), 64)
	return number
}

type noteLength struct {
	denominator float64
	dots        int32
}

func terminatesNoteLength(c rune) bool {
	return c == ' ' || c == '\r' || c == '\n' || c == '/' || c == '~'
}

func (s *scanner) parseNoteLength() {
	// NB: This assumes that the first digit has already been consumed.

	// Consume the rest of the digits.
	s.consumeDigits()

	// Look for a fractional part.
	if s.peek() == '.' && isDigit(s.peekNext()) {
		// Consume the decimal.
		s.advance()
		// Consume digits to the right of the decimal.
		s.consumeDigits()
	}

	number := s.parseFloatFrom(s.start)

	c := s.peek()
	n := s.peekNext()
	if c == 's' && (terminatesNoteLength(n) || s.eofIsNext()) {
		// consume 's'
		s.advance()
		s.addToken(NoteLengthMs, number*1000)
		return
	}

	if c == 'm' && n == 's' {
		// consume 'm' and 's'
		s.advance()
		s.advance()
		s.addToken(NoteLengthMs, number)
		return
	}

	dots := 0

	for c := s.peek(); c == '.'; c = s.peek() {
		dots++
		s.advance()
	}

	s.addToken(NoteLength, noteLength{denominator: number, dots: int32(dots)})
}

func (s *scanner) parseInteger() {
	s.consumeDigits()
	s.addToken(Integer, s.parseIntegerFrom(s.start))
}

// This assumes that the initial digit (or minus sign, if it's a negative
// number) was already consumed.
func (s *scanner) parseNumber() {
	// Parse numbers before the period.
	s.consumeDigits()

	// Look for a fractional part.
	if s.peek() == '.' && isDigit(s.peekNext()) {
		s.advance()
	}

	// Parse numbers after the period.
	s.consumeDigits()

	s.addToken(Number, s.parseFloatFrom(s.start))
}

type repetitionRange struct {
	first int32
	last  int32
}

func (s *scanner) parseRepetitions() error {
	// NB: This assumes the initial "'" was already consumed.

	ranges := []repetitionRange{}

	// Parse repetition ranges as long as we continue to encounter them.
	for {
		if c := s.peek(); !isDigit(c) {
			return s.unexpectedCharError(c, "in repetitions", s.line, s.column)
		}

		// Parse the "first" number of the range.
		startNumber := s.current
		s.consumeDigits()
		first := s.parseIntegerFrom(startNumber)
		er := repetitionRange{first: first}

		// Either parse the "last" number of the range, or make the first number
		// the last number as well, indicating a range of one number, e.g. 3-3.
		if s.match('-') {
			// Make sure a number comes next.
			if c := s.peek(); !isDigit(c) {
				return s.unexpectedCharError(c, "in repetitions", s.line, s.column)
			}

			startNumber := s.current
			s.consumeDigits()
			er.last = s.parseIntegerFrom(startNumber)
		} else {
			er.last = first
		}

		ranges = append(ranges, er)

		// At this point, there could be a comma, indicating there are more ranges
		// to parse. Only in that case do we continue to loop.
		if !s.match(',') {
			break
		}
	}

	s.addToken(Repetitions, ranges)
	return nil
}

func (s *scanner) parseRepeat() error {
	// NB: This assumes the initial '*' was already consumed.

	s.consumeSpaces()

	if c := s.peek(); !isDigit(c) {
		return s.unexpectedCharError(c, "in repeat", s.line, s.column)
	}

	startDigits := s.current
	s.consumeDigits()

	if c := s.peek(); c != ' ' &&
		c != '\r' &&
		c != '\n' &&
		c != ']' &&
		c != '}' &&
		!s.reachedEOF() {
		return s.unexpectedCharError(c, "in repeat", s.line, s.column)
	}

	digits := s.input[startDigits:s.current]
	times, _ := strconv.ParseInt(string(digits), 10, 32)
	s.addToken(Repeat, int32(times))

	return nil
}

func (s *scanner) parseOctaveSet() error {
	// NB: This assumes the initial 'o' was already consumed.

	s.consumeDigits()

	if c := s.peek(); c != ' ' && c != '\r' && c != '\n' && !s.reachedEOF() {
		return s.unexpectedCharError(c, "in octave set", s.line, s.column)
	}

	// Trim the initial 'o'
	digits := s.input[s.start+1 : s.current]
	octaveNumber, _ := strconv.ParseInt(string(digits), 10, 32)
	s.addToken(OctaveSet, int32(octaveNumber))

	return nil
}

func (s *scanner) parseVoiceMarker() error {
	// NB: This assumes the initial 'V' was already consumed.

	s.consumeDigits()

	if c := s.peek(); c != ':' {
		return s.unexpectedCharError(c, "in voice marker", s.line, s.column)
	}

	// Consume the final ':'
	s.advance()

	// Trim the surrounding 'V' and ':'.
	digits := s.input[s.start+1 : s.current-1]
	voiceNumber, _ := strconv.ParseInt(string(digits), 10, 32)
	s.addToken(VoiceMarker, int32(voiceNumber))

	return nil
}

func isValidSymbolChar(c rune) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '(', ')', '[', ']', '{', '}', '"':
		return false
	}

	return true
}

func (s *scanner) parseSymbol() {
	s.consumeWhile(isValidSymbolChar)
	s.addToken(Symbol, nil)
}

func isValidNameChar(c rune) bool {
	if isLetter(c) || isDigit(c) {
		return true
	}

	switch c {
	case '_', '-', '+', '\'', '(', ')', '.':
		return true
	}

	return false
}

func (s *scanner) parseName() {
	s.consumeWhile(isValidNameChar)
	s.addToken(Name, nil)
}

func (s *scanner) parseAlias() error {
	// NB: This assumes the initial double quote was already consumed.

	s.consumeWhile(isValidNameChar)

	if s.reachedEOF() {
		return s.errorAtPosition(s.line, s.column, "Unterminated alias")
	}

	if c := s.peek(); c != '"' {
		s.unexpectedCharError(c, "in alias", s.line, s.column)
	}

	// Consume the closing ".
	s.advance()

	// Trim the surrounding quotes.
	contents := s.input[s.start+1 : s.current-1]
	s.addToken(Alias, string(contents))

	return nil
}

func (s *scanner) parsePrefixedName(
	tokenType TokenType, contextName string) error {
	// NB: This assumes the initial prefix character was already consumed.

	if c := s.peek(); !isValidNameChar(c) {
		return s.unexpectedCharError(c, contextName, s.line, s.column)
	}

	s.consumeWhile(isValidNameChar)

	// Trim the initial prefix character
	s.addToken(tokenType, string(s.input[s.start+1:s.current]))

	return nil
}

func (s *scanner) parseMarker() error {
	return s.parsePrefixedName(Marker, "in marker name")
}

func (s *scanner) parseAtMarker() error {
	return s.parsePrefixedName(AtMarker, "in marker name")
}

func isNoteLetter(c rune) bool {
	return 'a' <= c && c <= 'g'
}

func isRestLetter(c rune) bool {
	return c == 'r'
}

func isOctaveLetter(c rune) bool {
	return c == 'o'
}

func isVoiceLetter(c rune) bool {
	return c == 'V'
}

func followsNoteLetter(c rune) bool {
	if isDigit(c) {
		return true
	}

	switch c {
	case '#', ' ', '\r', '\n', '+', '-', '_', '/', '~', '*', '\'', '}', ']', '<',
		'>':
		return true
	}

	return false
}

func followsRestLetter(c rune) bool {
	if isDigit(c) {
		return true
	}

	switch c {
	case '#', ' ', '\r', '\n', '/', '~', '*', '\'', '}', ']', '<', '>':
		return true
	}

	return false
}

func (s *scanner) scanToken() error {
	prevLine := s.line
	prevColumn := s.column

	c := s.advance()

	switch c {
	case ' ', '\r', '\n', '\t':
		// skip whitespace
		return nil
	case '#':
		s.skipComment() // TODO: scan comments, only drop at the parsing level
		return nil
	case '(':
		s.sexpLevel++
		s.addToken(LeftParen, nil)
		return nil
	case ')':
		s.sexpLevel--
		s.addToken(RightParen, nil)
		return nil
	}

	// If we're inside of parentheses, we are in "lisp mode," which has different
	// syntax rules.
	if s.sexpLevel > 0 {
		var err error

		switch c {
		case '\'':
			s.addToken(SingleQuote, nil)
		case '"':
			err = s.parseString()
		default:
			switch {
			case c == '-' || isDigit(c):
				s.parseNumber()
			case isValidSymbolChar(c):
				s.parseSymbol()
			default:
				return s.unexpectedCharError(c, "in S-expression", prevLine, prevColumn)
			}
		}
		return err
	}

	var err error

	switch c {
	case '{':
		s.addToken(CramOpen, nil)
	case '}':
		s.addToken(CramClose, nil)
	case '[':
		s.addToken(EventSeqOpen, nil)
	case ']':
		s.addToken(EventSeqClose, nil)
	case '-':
		s.addToken(Flat, nil)
	case '+':
		s.addToken(Sharp, nil)
	case '_':
		s.addToken(Natural, nil)
	case '/':
		s.addToken(Separator, nil)
	case '<':
		s.addToken(OctaveDown, nil)
	case '>':
		s.addToken(OctaveUp, nil)
	case ':':
		s.addToken(Colon, nil)
	case '~':
		s.addToken(Tie, nil)
	case '|':
		s.addToken(Barline, nil)
	case '=':
		s.addToken(Equals, nil)
	case '\'':
		err = s.parseRepetitions()
	case '*':
		err = s.parseRepeat()
	case '"':
		err = s.parseAlias()
	case '%':
		err = s.parseMarker()
	case '@':
		err = s.parseAtMarker()
	default:
		switch {
		case isDigit(c):
			s.parseNoteLength()
		case isLetter(c):
			n := s.peek()
			switch {
			case isLetter(n):
				s.parseName()
			case isNoteLetter(c) && (followsNoteLetter(n) || s.reachedEOF()):
				s.addToken(NoteLetter, c)
			case isRestLetter(c) && (followsRestLetter(n) || s.reachedEOF()):
				s.addToken(RestLetter, nil)
			case isOctaveLetter(c) && (isDigit(n) || s.reachedEOF()):
				err = s.parseOctaveSet()
			case isVoiceLetter(c) && isDigit(n):
				err = s.parseVoiceMarker()
			default:
				return s.unexpectedCharError(n, "in note/rest/name", s.line, s.column)
			}
		default:
			return s.unexpectedCharError(c, "at the top level", prevLine, prevColumn)
		}
	}

	return err
}

// Scan an input string and return a list of tokens.
//
// The `filename` argument is included in the error message in the event of a
// parse error.
func Scan(filename string, input string) ([]Token, error) {
	s := newScanner(filename, input)
	for !s.reachedEOF() {
		// We are at the beginning of the next lexeme.
		s.start = s.current
		s.startLine = s.line
		s.startColumn = s.column

		// log.Debug().
		// 	Int("line", s.line).
		// 	Int("column", s.column).
		// 	Int("sexpLevel", s.sexpLevel).
		// 	Str("atCharacter", string(s.peek())).
		// 	Msg("Scanning token.")
		// Scan the next token.
		if err := s.scanToken(); err != nil {
			return nil, err
		}
	}

	s.tokens = append(s.tokens, Token{
		TokenType: EOF,
		Text:      "",
		Literal:   nil,
		SourceContext: model.AldaSourceContext{
			Filename: s.filename,
			Line:     s.line,
			Column:   s.column,
		},
	})

	return s.tokens, nil
}

// ScanFile reads a file, scans it, and returns a list of tokens.
func ScanFile(filepath string) ([]Token, error) {
	contents, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	return Scan(filepath, string(contents))
}
