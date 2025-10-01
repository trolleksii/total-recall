package ollama

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"total-recall/internal/config"
	"total-recall/internal/types"
)

// expandPath expands tilde (~) to the user's home directory
func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(homeDir, path[1:])
}

// loadTemplate loads a template from file with fallback to default content
func loadTemplate(templatePath, defaultContent string) string {
	expandedPath := expandPath(templatePath)

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		// If template file doesn't exist, return default content
		return defaultContent
	}

	return string(data)
}

const summaryPromptTmplDefault string = `
You are a DevOps expert. Summarize the following terminal command into a clear, actionable intent statement.

Common alias reference:
alias l='eza -1A --group-directories-first'
alias kat="kubectl apply -f -<<'EOF'"
alias kk='k9s --headless --crumbsless --splashless'
alias kkx='k9s --headless --crumbsless --splashless --context $(yq ".contexts.[].name" ~/.kube/config | fzf)'
alias add='git add'
alias commit='git commit'
alias clone='git clone'
alias push='git push'
alias pull='git pull'
alias rebase='git rebase'
alias nv='nvim'
alias code='nvim $(rg --files --hidden --glob "!.git/*" | fzf)'
alias ns='tmux-sessionizer'

Rules:
- Focus on WHAT the user wants to accomplish, not HOW
- Include key technical details (tool names, resource types, etc)
- Exclude the irrelevant technical details(eg tags, versions, port numbers, kubernetes namespaces, environments, etc)
- Use consistent terminology (e.g., always "deploy" not "apply/create/run")
- If you don't know what specific tool is doing simply respond "Run the <TOOL_NAME> with <ARGUMENTS LIST>"
- Keep it 1-2 sentences maximum

Examples:
Command: kubectl get pods -n production --selector=app=nginx -o wide
Intent: List nginx application pods in with detailed information

Command: helm fetch --untar fission-charts/fission-all --version 1.20.5
Intent: Download and extract a Helm chart fission-charts/fission-all

Command: curl -X GET -H "Content-Type: application/json" -H "DD_API_KEY: 11111111111111111111111111111111" -H "DD_APPLICATION_KEY: 2222222222222222222222222222222222222222" "https://api.datadoghq.com/api/v1/synthetics/tests/browser/wap-wfv-ieu/results/3333333333333333333"
Intent: Make a HTTP GET request with headers to api.datadoghq.com

Command: docker run --rm -it quay.io/argoproj/argocd:latest /bin/bash
Intent: Run a temporary argocd container and run an interactive bash shell

Command: kat
apiVersion: v1
kind: Pod
metadata:
  name: redis-cli
  namespace: bifrost
spec:
  containers:
  - command:
    - tail
    - -f
    - /dev/null
    image: redis:7
    name: redis
  tolerations:
  - effect: NoSchedule
    key: bifrost
    operator: Exists
  nodeSelector:
    project: bifrost
EOF
Intent: Deploy a redis-cli pod with tolerations and nodeSelector defined

Command: %s
Intent: `

const rankingPromptTmplDefault string = `
You are a DevOps expert. A user wants to find a command that will: "%s".

I found these commands from their zsh history :
%s

Here is a reference of some common command aliases:
alias l='eza -1A --group-directories-first'
alias kat="kubectl apply -f -<<'EOF'"
alias kk='k9s --headless --crumbsless --splashless'
alias kkx='k9s --headless --crumbsless --splashless --context $(yq ".contexts.[].name" ~/.kube/config | fzf)'
alias add='git add'
alias commit='git commit'
alias clone='git clone'
alias push='git push'
alias pull='git pull'
alias rebase='git rebase'
alias nv='nvim'
alias code='nvim $(rg --files --hidden --glob "!.git/*" | fzf)'
alias ns='tmux-sessionizer'

Your task is to rank the commands by relevance to the user's request and return the numbers of the top 3 most relevant commands.

You have to ground your ranking based on:
- Semantic similarity to the request
- Technical appropriateness 
- Likely user intent
- Recency and usefulness

Your response must only contain the numbers of the top 3 commands separated by spaces (e.g., "5 12 8"). Do not include any other text, explanations, or formatting.

Response: `

const toolDetectionPromptTmplDefault string = `
You are a DevOps expert. Analyze the following shell command and identify the PRIMARY tool or technology being used.

Shell command:
%s

Your task is to identify the main tool/application (e.g., kubectl, docker, curl, jq, helm, git, terraform, etc.).

Rules:
- Return ONLY the tool name, nothing else
- If the command uses multiple tools, return the most significant one
- If it's a basic shell command (cd, ls, echo, etc.), return "bash"
- If you cannot identify a specific tool, return "bash"
- Do not include version numbers or additional text

Examples:
Command: kubectl get pods -n production
Response: kubectl

Command: docker run -it ubuntu bash
Response: docker

Command: curl -X POST https://api.example.com/data | jq '.results[]'
Response: curl

Command: helm install my-release stable/nginx
Response: helm

Command: ls -la | grep .txt
Response: bash

Response: `

const refinementPromptTmplDefault string = `
You are a DevOps expert. Consider the following shell command:
%s

The user wants to update it and asks you to %s.

%s

Follow the user's instructions.
You need to consider the following:
- The original command's purpose and structure
- How the refinement modifies or extends the functionality
- DevOps best practices and common patterns
- Make the commands practical and executable
- Use the documentation provided above for accurate syntax and options

Generate 3 distinct command variations that address the refinement. Each should be a complete, executable command.

Examples:
- Original shell command: "docker ps"
  Refinement request: "show only running containers with names"
  Refined shell command: "docker ps --filter status=running --format 'table {{.Names}}\t{{.Status}}'"
- Original shell command: "kubectl get pods"
  Refinement request: "in production namespace with labels"
  Refined shell command: "kubectl get pods -n production --show-labels"

Your response must contain exactly 3 commands. For multi-line commands, use proper line continuation with backslashes (\).

Format your response as:
command1
---COMMAND---
command2
---COMMAND---
command3

Commands:
`

func ComposeSummaryPrompt(text string) string {
	cfg := config.Get()
	template := loadTemplate(cfg.Prompts.SummaryTemplate, summaryPromptTmplDefault)
	return fmt.Sprintf(template, text)
}

func ComposeRankingPrompt(query string, commands []types.EmbeddedCommand) string {
	var sb strings.Builder
	for i, cmd := range commands {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, cmd.Text))
	}

	cfg := config.Get()
	template := loadTemplate(cfg.Prompts.RankingTemplate, rankingPromptTmplDefault)
	return fmt.Sprintf(template, query, sb.String())
}

func ComposeToolDetectionPrompt(command string) string {
	cfg := config.Get()
	template := loadTemplate(cfg.Prompts.ToolDetectionTemplate, toolDetectionPromptTmplDefault)
	return fmt.Sprintf(template, command)
}

func ComposeRefinementPrompt(selectedCommand, refinementQuery string) string {
	cfg := config.Get()
	template := loadTemplate(cfg.Prompts.RefinementTemplate, refinementPromptTmplDefault)
	return fmt.Sprintf(template, selectedCommand, refinementQuery, "")
}

func ComposeRefinementPromptWithDocs(selectedCommand, refinementQuery, documentation string) string {
	cfg := config.Get()
	template := loadTemplate(cfg.Prompts.RefinementTemplate, refinementPromptTmplDefault)

	docsSection := ""
	if documentation != "" {
		docsSection = fmt.Sprintf("Relevant documentation:\n%s\n", documentation)
	}

	return fmt.Sprintf(template, selectedCommand, refinementQuery, docsSection)
}
