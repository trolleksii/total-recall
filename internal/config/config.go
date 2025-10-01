package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Qdrant struct {
		Host               string `yaml:"host"`
		Port               int    `yaml:"port"`
		CommandCollection  string `yaml:"commandCollection"`
		VectorDimensions   int    `yaml:"vectorDimensions"`
	} `yaml:"qdrant"`

	Ollama struct {
		URL             string `yaml:"url"`
		EmbeddingModel  string `yaml:"embeddingModel"`
		GenerationModel string `yaml:"generationModel"`
	} `yaml:"ollama"`

	Application struct {
		DefaultHistoryFile string `yaml:"historyFile"`
		TopK               int    `yaml:"top_k"`
	} `yaml:"application"`

	Prompts struct {
		SummaryTemplate    string `yaml:"summaryTemplate"`
		RankingTemplate    string `yaml:"rankingTemplate"`
		RefinementTemplate string `yaml:"refinementTemplate"`
	} `yaml:"prompts"`
}

var cfg *Config

func Load() (*Config, error) {
	if cfg != nil {
		return cfg, nil
	}

	cfg = &Config{}

	// Set default values
	cfg.Qdrant.Host = "localhost"
	cfg.Qdrant.Port = 6334
	cfg.Qdrant.CommandCollection = "zsh_commands"
	cfg.Qdrant.VectorDimensions = 1024

	cfg.Ollama.URL = "http://192.168.0.10:11434"
	cfg.Ollama.EmbeddingModel = "dengcao/Qwen3-Embedding-0.6B:F16"
	cfg.Ollama.GenerationModel = "qwen3:latest"

	cfg.Application.DefaultHistoryFile = "~/.zsh_history"
	cfg.Application.TopK = 15

	cfg.Prompts.SummaryTemplate = "~/.config/total-recall/summary_prompt.tmpl"
	cfg.Prompts.RankingTemplate = "~/.config/total-recall/ranking_prompt.tmpl"
	cfg.Prompts.RefinementTemplate = "~/.config/total-recall/refinement_prompt.tmpl"

	// Try to load from config file
	configPath := getConfigPath()
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
		}
	}

	return cfg, nil
}

func getConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "total-recall.yaml"
	}
	return filepath.Join(homeDir, ".config", "total-recall", "config.yaml")
}

func Get() *Config {
	if cfg == nil {
		var err error
		cfg, err = Load()
		if err != nil {
			panic(fmt.Sprintf("failed to load config: %v", err))
		}
	}
	return cfg
}
