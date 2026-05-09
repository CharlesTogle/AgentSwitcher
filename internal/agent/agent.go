package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Kind string

const (
	Codex  Kind = "codex"
	Claude Kind = "claude"
	Gemini Kind = "gemini"
	Pi     Kind = "pi"
)

type Definition struct {
	Kind        Kind
	Label       string
	Description string
	Binary      string
}

type Runner struct {
	def Definition
}

type Result struct {
	Output string
	Stderr string
}

func Definitions() []Definition {
	return []Definition{
		{
			Kind:        Codex,
			Label:       "Codex",
			Description: "OpenAI Codex CLI",
			Binary:      "codex",
		},
		{
			Kind:        Claude,
			Label:       "Claude",
			Description: "Anthropic Claude Code",
			Binary:      "claude",
		},
		{
			Kind:        Gemini,
			Label:       "Gemini",
			Description: "Google Gemini CLI",
			Binary:      "gemini",
		},
		{
			Kind:        Pi,
			Label:       "Pi",
			Description: "Pi coding agent",
			Binary:      "pi",
		},
	}
}

func Find(kind Kind) (Definition, bool) {
	for _, def := range Definitions() {
		if def.Kind == kind {
			return def, true
		}
	}
	return Definition{}, false
}

func NewRunner(kind Kind) (Runner, error) {
	def, ok := Find(kind)
	if !ok {
		return Runner{}, fmt.Errorf("unknown agent: %s", kind)
	}
	return Runner{def: def}, nil
}

func (r Runner) Definition() Definition {
	return r.def
}

func (r Runner) Run(ctx context.Context, prompt string) (Result, error) {
	outputFilePath := ""
	if r.def.Kind == Codex {
		file, err := os.CreateTemp("", "agentswitcher-codex-last-message-*.txt")
		if err != nil {
			return Result{}, fmt.Errorf("create codex output file: %w", err)
		}
		outputFilePath = file.Name()
		_ = file.Close()
		defer os.Remove(outputFilePath)
	}

	args := buildArgs(r.def.Kind, prompt, outputFilePath)
	cmd := exec.CommandContext(ctx, r.def.Binary, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		Output: selectOutput(r.def.Kind, strings.TrimSpace(stdout.String()), outputFilePath),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if err != nil {
		if result.Stderr == "" {
			result.Stderr = err.Error()
		}
		return result, fmt.Errorf("%s failed: %w", r.def.Label, err)
	}
	if result.Output == "" && result.Stderr != "" {
		result.Output = result.Stderr
	}
	return result, nil
}

func buildArgs(kind Kind, prompt string, outputFilePath string) []string {
	switch kind {
	case Codex:
		args := []string{"exec", prompt, "--skip-git-repo-check", "--color", "never"}
		if outputFilePath != "" {
			args = append(args, "--output-last-message", outputFilePath)
		}
		return args
	case Claude:
		return []string{"-p", prompt}
	case Gemini:
		return []string{"-p", prompt}
	case Pi:
		return []string{"-p", prompt}
	default:
		return []string{prompt}
	}
}

func selectOutput(kind Kind, stdout string, outputFilePath string) string {
	if kind != Codex {
		return stdout
	}

	if outputFilePath != "" {
		content, err := os.ReadFile(outputFilePath)
		if err == nil {
			trimmed := strings.TrimSpace(string(content))
			if trimmed != "" {
				return sanitizeCodexOutput(trimmed)
			}
		}
	}

	return sanitizeCodexOutput(stdout)
}

func sanitizeCodexOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	start := 0
	for i := len(lines) - 1; i >= 0; i-- {
		switch strings.ToLower(strings.TrimSpace(lines[i])) {
		case "codex", "assistant":
			start = i + 1
			goto foundStart
		}
	}

foundStart:
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.ToLower(strings.TrimSpace(lines[i])) == "tokens used" {
			end = i
			break
		}
	}

	sanitized := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if sanitized == "" {
		return trimmed
	}
	return sanitized
}
