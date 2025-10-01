package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"total-recall/internal/types"
)

var trivialCommands = []string{"export", "code", "kx", "kkx", "mkdir", "mktemp", "make", "rm", "du", "df", "kill", "cd", "cp", "mv", "ls", "l", "recall", "ingest", "./recal", "./ingest"}

// zsh history format: ": timestamp:duration;command"
var zshNewCommandRegex = regexp.MustCompile(`^: (\d+):\d+;(.*)$`)

// ParseHistoryFile parses a zsh history file and returns commands with timestamps
func ParseHistoryFile(historyPath string) ([]types.Command, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(historyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		historyPath = filepath.Join(home, historyPath[2:])
	}

	file, err := os.Open(historyPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var commands []types.Command
	var currentCommand strings.Builder
	var currentTimestamp int64

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		matches := zshNewCommandRegex.FindStringSubmatch(line)
		if len(matches) == 3 {
			if currentCommand.Len() > 0 {
				cc := currentCommand.String()
				currentCommand.Reset()
				if !utf8.Valid([]byte(cc)) {
					//skip commands containing non-utf characters
					continue
				}
				if isTrivialCommand(cc) {
					// skip trivial commands
					continue
				}
				commands = append(commands, types.Command{
					Text:      strings.TrimSpace(strings.ReplaceAll(cc, "\\\n", "\n")),
					Timestamp: time.Unix(currentTimestamp, 0),
				})
			}
			currentTimestamp, err = strconv.ParseInt(matches[1], 10, 64)
			currentCommand.WriteString(matches[2])
		} else {
			if currentCommand.Len() > 0 {
				currentCommand.WriteString("\n")
				currentCommand.WriteString(line)
			}
		}
	}
	if currentCommand.Len() > 0 {
		commands = append(commands, types.Command{
			Text:      strings.TrimSpace(strings.ReplaceAll(currentCommand.String(), "\\\n", "\n")),
			Timestamp: time.Unix(currentTimestamp, 0),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return commands, nil
}

func isTrivialCommand(cmd string) bool {
	tokens := strings.Split(cmd, " ")
	return slices.Contains(trivialCommands, tokens[0])
}
