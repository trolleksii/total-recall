package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"total-recall/internal/config"
	"total-recall/internal/context7"
	"total-recall/internal/ollama"
	"total-recall/internal/parser"
	"total-recall/internal/qdrant"
	"total-recall/internal/types"
)

func main() {
	// Load config first to get defaults
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	var historyFile string
	var ingest bool
	flag.StringVar(&historyFile, "history-file", cfg.Application.DefaultHistoryFile, "Path to zsh history file")
	flag.BoolVar(&ingest, "ingest", false, "Whether to run in ingestion mode")
	flag.Parse()

	// Get query from command line arguments
	args := flag.Args()
	if !ingest && len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <query>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s \"find large files\"\n", os.Args[0])
		os.Exit(1)
	}

	// Join all arguments to form the query
	query := strings.Join(args, " ")
	if !ingest && query == "" {
		fmt.Fprintf(os.Stderr, "Error: Query cannot be empty\n")
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize clients
	ollamaClient := ollama.NewClient()
	qdrantClient, err := qdrant.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Qdrant client: %v", err)
	}

	if ingest {
		ingestData(ctx, ollamaClient, qdrantClient, historyFile)
		return
	}

	runTUI(ctx, query, ollamaClient, qdrantClient, cfg)
}

// TUI application states
type tuiState int

const (
	stateNormal tuiState = iota
	stateInput
	stateLoading
)

// tuiModel represents the TUI application state
type tuiModel struct {
	ctx              context.Context
	originalQuery    string
	commands         []types.EmbeddedCommand
	state            tuiState
	textInput        textinput.Model
	spinner          spinner.Model
	paginator        paginator.Model
	viewport         viewport.Model
	ollamaClient     *ollama.Client
	qdrantClient     *qdrant.Client
	context7Client   *context7.Client
	cfg              *config.Config
	error            error
	width            int
	height           int
	// Exit handling
	selectedCommand  string
	shouldOpenEditor bool
}

// runTUI starts the TUI interface
func runTUI(ctx context.Context, query string, ollamaClient *ollama.Client, qdrantClient *qdrant.Client, cfg *config.Config) {
	// Perform initial search
	commands, err := searchCommands(ctx, query, ollamaClient, qdrantClient, cfg)
	if err != nil {
		log.Fatalf("Failed to search commands: %v", err)
	}

	if len(commands) == 0 {
		fmt.Fprintf(os.Stderr, "No similar commands found\n")
		os.Exit(1)
	}

	// Initialize text input for refinement queries
	ti := textinput.New()
	ti.Placeholder = "Enter refinement query..."
	ti.CharLimit = 156
	ti.Width = 74 // Initial width, will be updated dynamically on window resize

	// Initialize spinner
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Initialize paginator
	p := paginator.New()
	p.Type = paginator.Arabic
	p.PerPage = 1
	p.SetTotalPages(len(commands))

	// Initialize viewport
	vp := viewport.New(78, 8)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingRight(2)

	// Initialize Context7 client
	context7Client := context7.NewClient(cfg.Context7.APIKey)

	m := tuiModel{
		ctx:            ctx,
		originalQuery:  query,
		commands:       commands,
		state:          stateNormal,
		textInput:      ti,
		spinner:        s,
		paginator:      p,
		viewport:       vp,
		ollamaClient:   ollamaClient,
		qdrantClient:   qdrantClient,
		context7Client: context7Client,
		cfg:            cfg,
		width:          80,
		height:         24,
	}

	program := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}

	// Handle post-exit logic
	if model, ok := finalModel.(tuiModel); ok {
		if model.shouldOpenEditor && model.selectedCommand != "" {
			// Open editor after TUI cleanup
			handleEditorWorkflow(model.selectedCommand)
		} else if model.selectedCommand != "" {
			// Copy command to clipboard
			if copyToClipboard(model.selectedCommand) {
				fmt.Printf("📋 Command copied to clipboard: %s\n", model.selectedCommand)
			} else {
				fmt.Printf("Command: %s\n", model.selectedCommand)
				fmt.Printf("⚠️  Could not access clipboard - please copy manually\n")
			}
		}
	}
}

// handleEditorWorkflow opens the selected command in an editor
func handleEditorWorkflow(command string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi" // fallback
	}

	// Create a temporary file with the command
	tmpfile, err := os.CreateTemp("", "recall-*.sh")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp file: %v\n", err)
		return
	}

	if _, err := tmpfile.WriteString(command); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing to temp file: %v\n", err)
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		return
	}
	tmpfile.Close()

	// Open editor with proper terminal control
	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	// Clean up temp file
	os.Remove(tmpfile.Name())

	if err != nil {
		fmt.Fprintf(os.Stderr, "Editor exited with error: %v\n", err)
	}
}

// copyToClipboard attempts to copy text to system clipboard
func copyToClipboard(text string) bool {
	// Try different clipboard commands based on OS
	var cmd *exec.Cmd

	// macOS
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd = exec.Command("pbcopy")
	} else if _, err := exec.LookPath("xclip"); err == nil {
		// Linux with xclip
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		// Linux with xsel
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else {
		return false
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() == nil
}

// searchCommands performs the command search and ranking
func searchCommands(ctx context.Context, query string, ollamaClient *ollama.Client, qdrantClient *qdrant.Client, cfg *config.Config) ([]types.EmbeddedCommand, error) {
	// Generate embedding for the query
	queryVectors, err := ollamaClient.GenerateEmbeddings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Search for similar commands
	candidates, err := qdrantClient.SearchSimilar(ctx, queryVectors[0], cfg.Application.TopK)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar commands: %w", err)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no similar commands found")
	}

	prompt := ollama.ComposeRankingPrompt(query, candidates)
	response, err := ollamaClient.GenerateResponse(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to rank commands with LLM: %w", err)
	}

	// Parse the ranking response to get top 3 commands
	topCommands, err := parseRankingResponse(response, candidates)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ranking response: %w", err)
	}

	return topCommands, nil
}

// BubbleTea interface implementations
func (m tuiModel) Init() tea.Cmd {
	m.updateViewport()
	switch m.state {
	case stateInput:
		return textinput.Blink
	case stateLoading:
		return m.spinner.Tick
	}
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.state == stateInput {
			return m.handleInputMode(msg)
		}
		return m.handleNormalMode(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.textInput.Width = msg.Width - 6 // Match viewport content width (width - 4 for border, - 2 for padding)
		// Adjust viewport height based on input state
		baseHeight := msg.Height - 8
		if m.state == stateInput {
			// Reserve extra space for the input section (approximately 4 lines)
			baseHeight -= 4
		}
		m.viewport.Height = baseHeight
		m.updateViewport()
		return m, nil
	case commandsMsg:
		m.commands = msg.commands
		m.paginator.SetTotalPages(len(msg.commands))
		m.paginator.Page = 0
		m.state = stateNormal
		m.updateViewport()
		return m, nil
	case errorMsg:
		m.error = msg.error
		m.state = stateNormal
		return m, nil
	case spinner.TickMsg:
		if m.state == stateLoading {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	// Update paginator for navigation
	if m.state == stateNormal {
		m.paginator, cmd = m.paginator.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m tuiModel) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		// Scroll viewport up
		if len(m.commands) > 0 {
			m.viewport.ScrollUp(1)
		}
	case "down", "j":
		// Scroll viewport down
		if len(m.commands) > 0 {
			m.viewport.ScrollDown(1)
		}
	case "tab", "right", "l":
		if len(m.commands) > 0 {
			// Loop to beginning if at the end
			if m.paginator.Page >= len(m.commands)-1 {
				m.paginator.Page = 0
			} else {
				m.paginator.NextPage()
			}
			m.updateViewport()
		}
	case "shift+tab", "left", "h":
		if len(m.commands) > 0 {
			// Loop to end if at the beginning
			if m.paginator.Page <= 0 {
				m.paginator.Page = len(m.commands) - 1
			} else {
				m.paginator.PrevPage()
			}
			m.updateViewport()
		}
	case "enter":
		if len(m.commands) > 0 && m.paginator.Page < len(m.commands) {
			// Store command for output after TUI exits
			m.selectedCommand = m.commands[m.paginator.Page].Text
			return m, tea.Quit
		}
	case "r":
		// Repeat search with original query
		m.state = stateLoading
		return m, tea.Batch(m.repeatSearch(), m.spinner.Tick)
	case "e":
		// Mark for editor opening after TUI exits
		if len(m.commands) > 0 && m.paginator.Page < len(m.commands) {
			m.selectedCommand = m.commands[m.paginator.Page].Text
			m.shouldOpenEditor = true
			return m, tea.Quit
		}
	case "u":
		// Enter refinement input mode
		if len(m.commands) > 0 && m.paginator.Page < len(m.commands) {
			m.state = stateInput
			m.textInput.Focus()
			// Adjust viewport height for input mode
			m.viewport.Height = m.height - 12 // Extra space for input section
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m tuiModel) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Exit input mode
		m.state = stateNormal
		m.textInput.Blur()
		m.textInput.SetValue("")
		// Restore viewport height for normal mode
		m.viewport.Height = m.height - 8
		return m, nil
	case "enter":
		// Submit refinement query
		if m.textInput.Value() != "" {
			query := m.textInput.Value()
			m.textInput.SetValue("")
			m.textInput.Blur()
			m.state = stateLoading
			// Restore viewport height for normal mode
			m.viewport.Height = m.height - 8
			return m, tea.Batch(m.refineCommand(query), m.spinner.Tick)
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// updateViewport updates the viewport content with the current command
func (m *tuiModel) updateViewport() {
	if len(m.commands) == 0 {
		m.viewport.SetContent("No commands found")
		return
	}

	currentPage := m.paginator.Page
	if currentPage >= len(m.commands) {
		m.viewport.SetContent("Invalid command index")
		return
	}

	command := m.commands[currentPage]

	// Format multi-line commands with proper spacing and highlighting
	commandText := command.Text

	// Add syntax highlighting for common patterns while preserving indentation
	lines := strings.Split(commandText, "\n")
	var styledLines []string

	for _, line := range lines {
		// Keep original line for indentation, but get trimmed version for checking
		trimmedLine := strings.TrimSpace(line)

		if trimmedLine == "" {
			styledLines = append(styledLines, line) // Keep original spacing
			continue
		}

		// Style different parts of commands while preserving indentation
		if strings.HasPrefix(trimmedLine, "#") {
			// Comments in green - preserve indentation
			leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			styled := leadingSpace + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(trimmedLine)
			styledLines = append(styledLines, styled)
		} else if strings.Contains(trimmedLine, "&&") || strings.Contains(trimmedLine, "||") {
			// Logic operators in yellow - preserve indentation
			leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			styled := strings.ReplaceAll(trimmedLine, "&&", lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("&&"))
			styled = strings.ReplaceAll(styled, "||", lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("||"))
			styledLines = append(styledLines, leadingSpace+styled)
		} else {
			// Regular command text - keep original indentation
			styledLines = append(styledLines, line)
		}
	}

	formattedCommand := strings.Join(styledLines, "\n")
	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(formattedCommand)

	m.viewport.SetContent(content)
}

func (m tuiModel) View() string {
	var sections []string

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Align(lipgloss.Center).
		Width(m.width).
		Render("🔍 Total Recall - Command Search")
	sections = append(sections, title)

	// Query info
	queryInfo := lipgloss.NewStyle().
		Foreground(lipgloss.Color("248")).
		Width(m.width).
		Render(fmt.Sprintf("Query: %s", m.originalQuery))
	sections = append(sections, queryInfo)

	// Show refinement input at the top when in input state
	if m.state == stateInput {
		inputSection := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")). // Match viewport border color
			PaddingRight(2).                        // Match viewport padding
			Width(m.width - 6).                     // Match viewport total rendered width
			Render(lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("33")).
					Render("Refine selected command:"),
				"",
				m.textInput.View(),
			))
		sections = append(sections, inputSection)
	}

	// Handle different states
	switch m.state {
	case stateLoading:
		loadingView := lipgloss.JoinHorizontal(lipgloss.Left,
			m.spinner.View(),
			" Loading...")
		sections = append(sections, loadingView)

	default:
		// Always show command and navigation (even during input)
		if m.error != nil {
			errorMsg := lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render(fmt.Sprintf("Error: %v", m.error))
			sections = append(sections, errorMsg)
		} else if len(m.commands) == 0 {
			sections = append(sections, "No commands found")
		} else {
			// Paginator info
			paginatorView := lipgloss.NewStyle().
				Foreground(lipgloss.Color("248")).
				Render(m.paginator.View())
			sections = append(sections, paginatorView)

			// Command viewport
			sections = append(sections, m.viewport.View())
		}

		// Help text
		var help string
		if m.state == stateInput {
			help = "Press Enter to submit refinement, Esc to cancel"
		} else {
			help = "↑/↓: scroll • TAB/→: next • ←: prev • ENTER: copy & exit • R: repeat • E: edit & exit • U: refine • ESC: quit"
		}
		helpView := lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")).
			Render(help)
		sections = append(sections, helpView)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// Command functions
func (m tuiModel) repeatSearch() tea.Cmd {
	return func() tea.Msg {
		commands, err := searchCommands(m.ctx, m.originalQuery, m.ollamaClient, m.qdrantClient, m.cfg)
		if err != nil {
			return errorMsg{err}
		}
		return commandsMsg{commands}
	}
}

func (m tuiModel) refineCommand(refinementQuery string) tea.Cmd {
	return func() tea.Msg {
		selectedCommand := m.commands[m.paginator.Page].Text

		// Step 1: Detect the primary tool/technology using LLM
		toolDetectionPrompt := ollama.ComposeToolDetectionPrompt(selectedCommand)
		toolName, err := m.ollamaClient.GenerateResponse(m.ctx, toolDetectionPrompt)
		if err != nil {
			// Fallback: proceed without documentation
			return m.refineWithoutDocs(selectedCommand, refinementQuery)
		}

		toolName = strings.TrimSpace(toolName)

		// Step 2: Fetch documentation from Context7 API
		documentation := ""
		if toolName != "" && toolName != "bash" {
			// Resolve library ID
			libraryID, err := m.context7Client.ResolveLibraryID(m.ctx, toolName)
			if err == nil {
				// Fetch documentation with the refinement query as topic
				docs, err := m.context7Client.GetLibraryDocs(m.ctx, libraryID, refinementQuery)
				if err == nil {
					documentation = docs
				}
			}
			// Silently continue if Context7 fails - we'll use refinement without docs
		}

		// Step 3: Generate refinement with documentation context
		var prompt string
		if documentation != "" {
			prompt = ollama.ComposeRefinementPromptWithDocs(selectedCommand, refinementQuery, documentation)
		} else {
			prompt = ollama.ComposeRefinementPrompt(selectedCommand, refinementQuery)
		}

		// Get LLM response with new commands
		response, err := m.ollamaClient.GenerateResponse(m.ctx, prompt)
		if err != nil {
			return errorMsg{err}
		}

		// Parse the response to extract the 3 new commands
		newCommands := parseRefinementResponse(response)
		if len(newCommands) == 0 {
			return errorMsg{fmt.Errorf("no valid commands generated from refinement")}
		}

		// Convert to EmbeddedCommand format (without actual embeddings for now)
		embeddedCommands := make([]types.EmbeddedCommand, len(newCommands))
		for i, cmd := range newCommands {
			embeddedCommands[i] = types.EmbeddedCommand{
				Command: types.Command{
					Text:      cmd,
					Timestamp: time.Now(),
				},
				Vector: []float32{}, // Empty vector for refined commands
				ID:     fmt.Sprintf("refined-%d", i),
			}
		}

		return commandsMsg{embeddedCommands}
	}
}

// refineWithoutDocs is a fallback refinement without documentation
func (m tuiModel) refineWithoutDocs(selectedCommand, refinementQuery string) tea.Msg {
	prompt := ollama.ComposeRefinementPrompt(selectedCommand, refinementQuery)

	response, err := m.ollamaClient.GenerateResponse(m.ctx, prompt)
	if err != nil {
		return errorMsg{err}
	}

	newCommands := parseRefinementResponse(response)
	if len(newCommands) == 0 {
		return errorMsg{fmt.Errorf("no valid commands generated from refinement")}
	}

	embeddedCommands := make([]types.EmbeddedCommand, len(newCommands))
	for i, cmd := range newCommands {
		embeddedCommands[i] = types.EmbeddedCommand{
			Command: types.Command{
				Text:      cmd,
				Timestamp: time.Now(),
			},
			Vector: []float32{},
			ID:     fmt.Sprintf("refined-%d", i),
		}
	}

	return commandsMsg{embeddedCommands}
}


// parseRefinementResponse parses the LLM response containing new commands
func parseRefinementResponse(response string) []string {
	response = strings.TrimSpace(response)

	// First try delimiter-based parsing for multi-line commands
	if strings.Contains(response, "---COMMAND---") {
		parts := strings.Split(response, "---COMMAND---")
		var commands []string
		for i, part := range parts {
			if i > 2 {
				break
			}
			cmd := strings.TrimSpace(part)
			if cmd != "" {
				commands = append(commands, cmd)
			}
		}

		// Ensure we have at most 3 commands
		if len(commands) > 3 {
			commands = commands[:3]
		}
		return commands
	}

	// Fallback to line-by-line parsing for simple commands
	lines := strings.Split(response, "\n")
	var commands []string
	var currentCommand strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Remove numbering prefixes
		line = strings.TrimPrefix(line, "1. ")
		line = strings.TrimPrefix(line, "2. ")
		line = strings.TrimPrefix(line, "3. ")
		line = strings.TrimPrefix(line, "- ")

		if line == "" {
			continue
		}

		// Check if this looks like a new command (doesn't start with continuation chars)
		if !strings.HasPrefix(line, "&&") && !strings.HasPrefix(line, "||") &&
			!strings.HasPrefix(line, "|") && !strings.HasPrefix(line, "\\") &&
			currentCommand.Len() > 0 {
			// Save previous command and start new one
			commands = append(commands, currentCommand.String())
			currentCommand.Reset()
		}

		// Add to current command
		if currentCommand.Len() > 0 {
			currentCommand.WriteString("\n")
		}
		currentCommand.WriteString(line)
	}

	// Add the last command
	if currentCommand.Len() > 0 {
		commands = append(commands, currentCommand.String())
	}

	// Ensure we have at most 3 commands
	if len(commands) > 3 {
		commands = commands[:3]
	}

	return commands
}

// Custom message types
type errorMsg struct{ error }
type commandsMsg struct{ commands []types.EmbeddedCommand }

// parseRankingResponse parses the LLM response containing ranked command numbers
// and returns the top 3 commands from the candidates list
func parseRankingResponse(response string, candidates []types.EmbeddedCommand) ([]types.EmbeddedCommand, error) {
	response = strings.TrimSpace(response)

	// Split the response by spaces to get individual numbers
	parts := strings.Fields(response)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty ranking response")
	}

	// Take up to 3 numbers
	numToTake := min(3, len(parts))
	topCommands := make([]types.EmbeddedCommand, 0, numToTake)

	for i := range numToTake {
		// Convert string to number (1-based indexing from LLM)
		num, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil, fmt.Errorf("invalid rthisanking number '%s': %w", parts[i], err)
		}

		// Convert to 0-based index and validate
		idx := num - 1
		if idx < 0 || idx >= len(candidates) {
			return nil, fmt.Errorf("ranking number %d is out of range (1-%d)", num, len(candidates))
		}

		topCommands = append(topCommands, candidates[idx])
	}

	return topCommands, nil
}

func ingestData(ctx context.Context, ollamaClient *ollama.Client, qdrantClient *qdrant.Client, historyFile string) {
	// Create collections (will fail gracefully if already exists)
	log.Println("Creating Qdrant collections...")
	if err := qdrantClient.CreateCollection(ctx); err != nil {
		log.Printf("Commands collection creation failed (may already exist): %v", err)
	}
	if err := qdrantClient.CreateFeedbackCollection(ctx); err != nil {
		log.Printf("Feedback collection creation failed (may already exist): %v", err)
	}

	// Parse zsh history
	log.Println("Parsing zsh history...")
	commands, err := parser.ParseHistoryFile(historyFile)
	if err != nil {
		log.Fatalf("Failed to parse history file: %v", err)
	}
	log.Printf("Parsed %d commands", len(commands))

	// Process commands in batches to avoid overwhelming the system
	batchSize := 10
	processed := 0

	for i := 0; i < len(commands); i += batchSize {
		end := min(i+batchSize, len(commands))

		batch := commands[i:end]
		log.Printf("Processing batch %d-%d of %d commands", i+1, end, len(commands))

		if err := processCommandBatch(ctx, batch, ollamaClient, qdrantClient); err != nil {
			log.Printf("Failed to process command batch: %v", err)
			continue
		}
		processed += end - i

		log.Printf("Processed %d/%d commands", processed, len(commands))
	}

	log.Printf("Ingestion complete! Successfully processed %d commands", processed)
}

// processCommand generates embedding for a command and stores it in Qdrant
func processCommandBatch(ctx context.Context, batch []types.Command, ollamaClient *ollama.Client, qdrantClient *qdrant.Client) error {
	// Generate embedding
	summaries := make([]string, len(batch))
	for i := range len(batch) {
		cmd := batch[i]
		prompt := ollama.ComposeSummaryPrompt(cmd.Text)
		summary, err := ollamaClient.GenerateResponse(ctx, prompt)
		if err != nil {
			return err
		}
		summaries[i] = summary
	}
	vectors, err := ollamaClient.GenerateEmbeddings(ctx, summaries)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	embeddedCmds := make([]types.EmbeddedCommand, len(batch))
	for i, v := range vectors {
		embeddedCmds[i] = types.EmbeddedCommand{
			Command: batch[i],
			Vector:  v,
			ID:      generateUUID(),
		}
	}

	// Store in Qdrant
	if err := qdrantClient.StoreEmbeddings(ctx, embeddedCmds); err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	return nil
}

// generateUUID creates a random UUID string
func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
