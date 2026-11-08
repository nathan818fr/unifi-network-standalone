package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const supervisorOptions = "http://supervisor/addons/self/options"

// emptyResetAdmin empties Home Assistant's reset_admin setting once it was applied: left set, it
// would be forgotten there, and setting the same user again later would do nothing. Supervisor
// gets all the settings, as whether it merges a partial set isn't documented. They apply at the
// next start, which is all this needs.
func emptyResetAdmin(ctx context.Context, url, token, optionsFile string) error {
	content, err := os.ReadFile(optionsFile)
	if err != nil {
		return err
	}
	var options map[string]any
	if err := json.Unmarshal(content, &options); err != nil {
		return fmt.Errorf("reading %s: %w", optionsFile, err)
	}
	options["reset_admin"] = ""
	body, err := json.Marshal(map[string]any{"options": options})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the answer from Home Assistant: status %d", response.StatusCode)
	}
	return nil
}
