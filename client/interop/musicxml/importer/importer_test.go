package importer

import (
	"alda.io/client/model"
	"testing"

	_ "alda.io/client/testing"
)

func TestNotes(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple notes",
		file:  "../examples/note.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				> c4 d e f | g1
		`,
	})
}

func TestKeySignatures(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple traditional key signature",
		file:  "../examples/key_signature.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "f+ c+ g+ d+") 
				> c4 d e f | g1
		`,
	})
}

func TestAccidentals(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple accidentals",
		file:  "../examples/accidental.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				> c4 d+ e f- | g++1
		`,
	}, importerTestCase{
		label: "complex key signatures and accidentals (for postprocessing)",
		file:  "../examples/accidental2.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				(key-signature "f+ c+ g+ d+")
				> d4 d_ d+ d_ |
				(key-signature "b- e- a- d-")
				d4 d-- d++ d-8 d_ |
				d1
		`,
	})
}

func TestOctaves(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple octave switching",
		file:  "../examples/octave.musicxml",
		expected: `
midi-acoustic-grand-piano: 
	(key-signature "") 
	< b4 >> c < a > d | << b4 >>> d << b > e
`,
	})
}

func TestRests(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple rests",
		file:  "../examples/rest.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				> c2 r | d4 r e8 r f16 r g32 r r16 | a4 r2.
		`,
	})
}

func TestSlurs(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple slurs",
		file:  "../examples/slurs.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				c4 d~ e~ f~ | g4~ a b~ > c
		`,
	})
}

func TestChords(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple chords",
		file:  "../examples/chord.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				d2/g/b f2 | c1/e/g/>c
		`,
	})
}

func TestTies(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple ties",
		file:  "../examples/ties1.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				> d1~ | 1~ | 4 f8~8 f8 f < f8~8
		`,
	}, importerTestCase{
		label: "ties with chords",
		file:  "../examples/ties2.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				(key-signature "")
				a4/>c4~4 < g4 > d2~ | 4~4 e4~4/g
		`,
		postprocess: func(updates []model.ScoreUpdate) []model.ScoreUpdate {
			// The OctaveDown before the g is parsed into the chord
			// We take it out
			indexOfChord := 2

			updates[indexOfChord], _ = modifyNestedUpdates(
				updates[indexOfChord],
				func(updates []model.ScoreUpdate) []model.ScoreUpdate {
					return updates[:len(updates)-1]
				},
			)

			// Then we insert it in the parsed location
			updates = insert(
				model.AttributeUpdate{PartUpdate: model.OctaveDown{}},
				updates,
				indexOfChord+1,
			)

			// This change produces equivalent Alda representation
			// As we're just moving an octave change outside of the note itself
			return updates
		},
	}, importerTestCase{
		label: "complex ties",
		file:  "../examples/ties3.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				c4/e/g/>c4~4 e8/g8~8 d8~8
		`,
	})
}

func TestDots(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple dotted notes",
		file:  "../examples/dots.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "") 
				g4.... r64 a4... r32 | r4. r8 b2 ~ | 2. r8.. r32
		`,
	})
}

func TestVoices(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple voices",
		file:  "../examples/voices1.musicxml",
		expected: `
			midi-acoustic-grand-piano:  
				V1: (key-signature "") e4 f g a | b4 > c d e
				V2: c4 d e f | g4 a b > c
		`,
	}, importerTestCase{
		label: "complex voices with padding, backup, and forward",
		file:  "../examples/voices2.musicxml",
		expected: `
			midi-acoustic-grand-piano:  
				V1: (key-signature "") > c4 d e f | g4 a b > c
				V2: r1 | r2 > g
				V3: r4 g2 r4 | g1 
				V4: c4 r c r | r1 
		`,
	})
}

func TestParts(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple parts (wind quintet) with transpositions",
		file:  "../examples/parts.musicxml",
		expected: `
			midi-flute: 
				(key-signature "") 
				c4 d e f | g4 a b > c | < b4 a g f | e4 d c2
			midi-oboe: 
				(key-signature "") 
				c4 e g > c | < g4 e c2 | g1 | c1
			midi-clarinet: 
				(key-signature "f+ c+") 
				(transpose -2)
				d4 d d d | r1 | d4 d d d | r1
			midi-bassoon: 
				(key-signature "") 
				< c1 | g1 | c1 | c1
			midi-french-horn: 
				(key-signature "f+") 
				(transpose -7)
				g4 b g b | > d4 < b g2 | r1 | g1
		`,
	})
}

func TestRepeats(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple repeat",
		file:  "../examples/repeat1.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				[(key-signature "") > c1 | g1 <]*2
		`,
	}, importerTestCase{
		label: "simple repeat with forward repeat",
		file:  "../examples/repeat2.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				[(key-signature "") > c1 | g1 <]*2
		`,
	}, importerTestCase{
		label: "repeats with complex octave updates",
		file:  "../examples/repeat3.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				[(key-signature "") > d1 | > d1 <<]*2 | >> c1
		`,
	}, importerTestCase{
		label: "repeats with first and second ending",
		file:  "../examples/repeat4.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				[	
					(key-signature "")
					> c1 |
					[e1 <]'1
					| 
					[g1]'2
				]*2
		`,
	}, importerTestCase{
		label: "very complex repeats",
		file:  "../examples/repeat5.musicxml",
		expected: `
			midi-acoustic-grand-piano:
				[
					(key-signature "")
					> c1 | 
					[d1 <]'1
					|
					[e1 <]'2
					|
					[f1]'3
				]*3
				| [g1]*2 | [a1]*2
		`,
	})
}

func TestDynamics(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "simple dynamics",
		file:  "../examples/dynamics.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "")
				(f) > c4
				(ff) (mp) d
				e
				f
				| 
				g1 (p)
		`,
	})
}

func TestPercussion(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "percussion instruments",
		file:  "../examples/percussion.musicxml",
		expected: `
			midi-percussion "Triangle": 
				(key-signature "")
				o5 g+1 | o5 a1 | r1 | o5 g+1 |
				o5 a4 a g+ g+ 
			
			midi-percussion "Wood_Blocks": 
				(key-signature "")
				o5 e4 e f f |
				o5 e1 | o5 e1 | o5 f1 | r1
		`,
	})
}

func TestDurations(t *testing.T) {
	executeImporterTestCases(t, importerTestCase{
		label: "different redundant durations",
		file:  "../examples/duration.musicxml",
		expected: `
			midi-acoustic-grand-piano: 
				(key-signature "")
				c8~8 e c8~8/e/g e8~8 | 
				c2/e/g c4../e/g r16 | 
				c2 e | 
				[
					g2 r | 
					e2 c
				]*2 | 
				e2 r
		`,
	})
}
