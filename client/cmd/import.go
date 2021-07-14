package cmd

import (
	"alda.io/client/code_formatter"
	"fmt"
	"os"
	"strings"

	"alda.io/client/code_generator"
	"alda.io/client/color"
	"alda.io/client/help"
	"alda.io/client/interop/musicxml/importer"
	log "alda.io/client/logging"
	"alda.io/client/model"
	"github.com/spf13/cobra"
)

var outputAldaFilename string
var importFormat string

func init() {
	importCmd.Flags().StringVarP(
		&file, "file", "f", "", "Read data from a file to convert to Alda",
	)

	importCmd.Flags().StringVarP(
		&code, "code", "c", "", "Read data from a string to convert to Alda",
	)

	importCmd.Flags().StringVarP(
		&outputAldaFilename, "output", "o", "", "The output Alda code filename",
	)

	importCmd.Flags().StringVarP(
		&importFormat, "import-format", "i", "", "The format of the imported data",
	)
}

var importCmd = &cobra.Command{
	Use:    "import",
	Hidden: true,
	Short:  "Evaluate external format and import as Alda source code",
	Long: `Evaluate external format and import as Alda source code

---

Source code can be provided in one of three ways:

The path to a file (-f, --file):
  alda import -i musicxml -f path/to/my-score.musicxml -o my-score.alda

A string of code (-c, --code):
  alda import -i musicxml -c "...some musicxml data..." -o my-score.alda

Text piped into the process on stdin:
  echo "...some musicxml data..." | alda import -i musicxml -o my-score.alda

---

When -o / --output FILENAME is provided, the results are written into that file.

  alda import -i musicxml -c "...some musicxml data..." -o my-score.alda

Otherwise, the results are written to stdout, which is convenient for
redirecting into other files or processes.

  alda import -i musicxml -f path/to/my-score.musicxml > my-score.alda
  alda import -i musicxml -f path/to/my-score.musicxml | some-process > my-score.alda

---

Currently, the only import format is MusicXML.  

---`,
	RunE: func(_ *cobra.Command, args []string) error {
		if importFormat != "musicxml" {
			return help.UserFacingErrorf(
				`%s is not a supported input format.

Currently, the only supported output format is %s.`,
				color.Aurora.BrightYellow(importFormat),
				color.Aurora.BrightYellow("musicxml"),
			)
		}

		// TODO (experimental): remove warning log
		log.Warn().Msg(fmt.Sprintf(
			`The %s command is currently experimental. Imported scores may be incorrect and lack information.`,
			color.Aurora.BrightYellow("import"),
		))

		var scoreUpdates []model.ScoreUpdate
		var err error

		switch {
		case file != "":
			inputFile, err := os.Open(file)
			if err != nil {
				return err
			}

			scoreUpdates, err = importer.ImportMusicXML(inputFile)
		case code != "":
			reader := strings.NewReader(code)
			scoreUpdates, err = importer.ImportMusicXML(reader)

		default:
			bytes, err := readStdin()
			if err != nil {
				return err
			}

			reader := strings.NewReader(string(bytes))
			scoreUpdates, err = importer.ImportMusicXML(reader)
		}

		if err != nil {
			return err
		}

		tokens := code_generator.Generate(scoreUpdates)

		if outputAldaFilename == "" {
			// When no output filename is specified, we write directly to stdout
			code_formatter.Format(tokens, os.Stdout)
		} else {
			file, err := os.Create(outputAldaFilename)
			if err != nil {
				return err
			}

			code_formatter.Format(tokens, file)

			fmt.Fprintf(os.Stderr, "Imported score to %s\n", outputAldaFilename)
			if err := file.Close(); err != nil {
				return err
			}
		}

		return nil
	},
}
