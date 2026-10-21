package encode

import (
	"encoding/json"
	"fmt"
	"os"
)

func LoadFFmpegProfile(path, name string) (FFmpegProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FFmpegProfile{}, fmt.Errorf("read FFmpeg profiles %q: %w", path, err)
	}
	var profiles map[string]json.RawMessage
	if err := json.Unmarshal(data, &profiles); err != nil {
		return FFmpegProfile{}, fmt.Errorf("parse FFmpeg profiles %q: %w", path, err)
	}
	content, ok := profiles[name]
	if !ok {
		return FFmpegProfile{}, fmt.Errorf("FFmpeg profile %q not found in %q", name, path)
	}
	var selected struct {
		Input  *[]string `json:"input"`
		Output *[]string `json:"output"`
		Filter *string   `json:"filter"`
	}
	if err := json.Unmarshal(content, &selected); err != nil {
		return FFmpegProfile{}, fmt.Errorf("parse FFmpeg profile %q: %w", name, err)
	}
	if selected.Input == nil || selected.Output == nil {
		return FFmpegProfile{}, fmt.Errorf("FFmpeg profile %q must define input and output argument lists", name)
	}
	profile := FFmpegProfile{Input: *selected.Input, Output: *selected.Output}
	if selected.Filter != nil {
		profile.Filter = *selected.Filter
	}
	return profile, nil
}
