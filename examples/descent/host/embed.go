package host

import (
	"github.com/nooga/let-go/pkg/api"

	"github.com/mparrett/let-go-undertone/letgo"

	demolg "github.com/mparrett/let-go-undertone/examples/descent/lg"
)

// LoadEmbedded loads every namespace the demo needs with no filesystem:
// let-go-undertone's ambience first, then this demo's music, sfx and demo.
func LoadEmbedded(c *api.LetGo) error {
	if err := letgo.LoadEmbedded(c); err != nil {
		return err
	}
	for _, s := range demolg.Sources {
		if err := letgo.LoadSource(c, s.Name, s.Text); err != nil {
			return err
		}
	}
	return nil
}
