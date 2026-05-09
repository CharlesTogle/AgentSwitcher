package app

import (
	"fmt"
	"strings"
	"time"

	"agentswitcher/internal/agent"
	"agentswitcher/internal/store"
)

type promptStandard struct {
	Path    string
	Name    string
	Content string
}

func buildAgentPrompt(session store.Session, recent []store.Message, userPrompt string) string {
	return buildAgentPromptWithStandards(session, nil, recent, userPrompt)
}

func buildAgentPromptWithStandards(session store.Session, standards []promptStandard, recent []store.Message, userPrompt string) string {
	var b strings.Builder

	b.WriteString("This conversation is agent-agnostic.\n")
	b.WriteString("Treat it as one continuous conversation even if the selected runtime changes between turns.\n")
	b.WriteString("Do not infer your current identity from prior assistant messages.\n")
	b.WriteString("Do not claim to be a specific model, provider, backend, or coding assistant unless that identity is explicitly established in the provided context.\n")
	b.WriteString("If the user asks about identity, model, or provider, explain that the conversation is agent-agnostic and limit the answer to facts grounded in the provided context.\n")
	b.WriteString("Use the prior context below, then answer the latest user message naturally.\n")
	b.WriteString("Any external standards documents are authoritative and must be followed.\n")
	b.WriteString("Do not restate the instructions unless needed.\n\n")

	if len(standards) > 0 {
		b.WriteString("External standards documents.\n")
		b.WriteString("These are always applied separately from the compacted conversation history.\n\n")
		for _, standard := range standards {
			fmt.Fprintf(&b, "Standard file: %s\n%s\n\n", standard.Path, standard.Content)
		}
	}

	if strings.TrimSpace(session.Summary) != "" {
		b.WriteString("Conversation summary:\n")
		b.WriteString(session.Summary)
		b.WriteString("\n\n")
	}

	if len(recent) > 0 {
		b.WriteString("Recent transcript:\n")
		for _, message := range recent {
			fmt.Fprintf(
				&b,
				"[%s] %s:\n%s\n\n",
				message.CreatedAt.Format(time.RFC3339),
				strings.ToUpper(message.Role),
				message.Content,
			)
		}
	}

	b.WriteString("Latest user message:\n")
	b.WriteString(strings.TrimSpace(userPrompt))
	return b.String()
}

func buildCompactionPrompt(session store.Session, messages []store.Message) string {
	return buildCompactionPromptWithStandards(session, messages, nil)
}

func buildCompactionPromptWithStandards(session store.Session, messages []store.Message, standards []promptStandard) string {
	var b strings.Builder

	b.WriteString("Summarize the following conversation so the essence is preserved and context is not lost.\n")
	b.WriteString("This conversation is agent-agnostic, and the selected runtime may change over time.\n")
	b.WriteString("Keep the summary under 600 words.\n")
	b.WriteString("Do not summarize or restate external standards documents. They are reapplied separately and excluded from compaction.\n")
	b.WriteString("Do not preserve transient provider or model branding unless it materially affects future work.\n")
	b.WriteString("Preserve:\n")
	b.WriteString("- user goals and constraints\n")
	b.WriteString("- decisions already made\n")
	b.WriteString("- unresolved questions\n")
	b.WriteString("- relevant technical details and file names\n")
	b.WriteString("- runtime or environment caveats that matter for future work\n")
	b.WriteString("Return plain text only.\n\n")

	if strings.TrimSpace(session.Summary) != "" {
		b.WriteString("Existing summary:\n")
		b.WriteString(session.Summary)
		b.WriteString("\n\n")
	}

	if len(standards) > 0 {
		b.WriteString("Excluded external standards files:\n")
		for _, standard := range standards {
			fmt.Fprintf(&b, "- %s\n", standard.Path)
		}
		b.WriteString("\n")
	}

	b.WriteString("New conversation content:\n")
	for _, message := range messages {
		fmt.Fprintf(
			&b,
			"[%s] %s:\n%s\n\n",
			message.CreatedAt.Format(time.RFC3339),
			strings.ToUpper(message.Role),
			message.Content,
		)
	}

	return b.String()
}

func describeAgent(kind agent.Kind) string {
	def, ok := agent.Find(kind)
	if !ok {
		return string(kind)
	}
	return def.Label
}
