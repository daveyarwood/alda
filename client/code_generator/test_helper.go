package code_generator

import (
	"alda.io/client/model"
	"alda.io/client/parser"
	"github.com/go-test/deep"
	"testing"
)

type generatorTestCase struct {
	label    string
	updates  []model.ScoreUpdate
	expected []parser.Token
}

func executeGeneratorTestCases(
	t *testing.T, testCases ...generatorTestCase,
) {
	for _, testCase := range testCases {
		actual := Generate(testCase.updates)

		if diff := deep.Equal(testCase.expected, actual); diff != nil {
			t.Error(testCase.label)
			for _, diffItem := range diff {
				t.Errorf("%v", diffItem)
			}
		}
	}
}
