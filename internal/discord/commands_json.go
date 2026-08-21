package discordx

import (
	"encoding/json"
)

func marshalCommands() ([]byte, error) {
	return json.Marshal(commands())
}
