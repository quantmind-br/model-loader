package backendschema

import (
	_ "embed"
	"encoding/json"

	"github.com/quantmind-br/model-loader/internal/domain"
)

//go:embed testdata/help-v9680.golden.json
var goldenHelpJSON []byte

func loadGoldenSchema() (domain.FlagSchema, error) {
	var fs domain.FlagSchema
	if err := json.Unmarshal(goldenHelpJSON, &fs); err != nil {
		return domain.FlagSchema{}, err
	}
	fs.Version = "embedded-v9680-full"
	return fs, nil
}
