package engine

// openai.go is the engine's network face for OpenAI-compatible endpoints:
// fetching the model list from a user-supplied GET {base}/models. The UI
// layer schedules the call and lands the result through its redraw bridge;
// this file only performs the HTTP request.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// FetchOpenAIModels calls GET {base}/models and returns the model ids,
// sorted. key, when non-empty, is sent as a Bearer token.
func FetchOpenAIModels(base, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
