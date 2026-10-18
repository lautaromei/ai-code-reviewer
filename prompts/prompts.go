package prompts

import (
	"embed"
	"fmt"
)

//go:embed *.txt
var files embed.FS

func load(task string, version string) (string, error) {
	name := fmt.Sprintf("%s_%s.txt", task, version)
	data, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompts: no template for task %q and version %q: %w", task, version, err)
	}
	return string(data), nil
}

func Classify(version string) (string, error) { return load("classify", version) }

func Compact(version string) (string, error) { return load("compact", version) }

func DraftRule(version string) (string, error) { return load("draft_rule", version) }
