# Claude Code Implementation Guide

# Instructions

You have access to the context7 MCP that can pull up-to-date, version-specific documentation and code examples straight from the source. Use it when the user requests code examples, setup or configuration steps, or library/API documentation.

## Project Overview

Build a prototype application that would use RAG of a zsh commands based on a user request and a command history.

## Core Requirements

1. Written in go(1.25.1)
2. Qdrant vector db
3. Self-hosted ollama( nomic-embed-text:latest for embeddings and llama3.1:8b for generation)
4. Simple structure hard-code configuration parameters when necessary

## Prototype Phase Specifications

### Data Source & Format
- **Command history source**: `.zsh_history` file
- **Data ingestion**: Manual trigger by user (batch processing)
- **Context data**: Timestamp (only readily available metadata)

### Query Interface & User Experience  
- **Input method**: CLI command `recall QUERY` where QUERY can be keywords or complex prompt
- **Response format**: Return historical command(s)

### Technical Architecture
- **Vector similarity**: Embed individual commands
- **Qdrant deployment**: Local Docker container  
- **Processing mode**: Batch ingestion as needed
- **User scope**: Single user system

### Scope & Features
- **Command filtering**: None (ingest all commands)
- **Error handling**: Basic logging sufficient for prototype

## Binary Architecture

### Binary 1: Data Ingestion (`ingest`)
- **Purpose**: Parse `.zsh_history` → generate embeddings → store in Qdrant
- **Usage**: `./ingest [--history-file ~/.zsh_history]`
- **Process**:
  1. Parse zsh history file (command + timestamp)
  2. Generate embeddings for raw command text via ollama (nomic-embed-text:latest)
  3. Store vectors + metadata in Qdrant
- **Configuration**: Hardcoded ollama/qdrant endpoints

### Binary 2: Query/Recall (`recall`)
- **Purpose**: Accept query → vector search → LLM ranking → return top commands
- **Usage**: `./recall "find large files"`
- **Process**:
  1. Generate query embedding via ollama
  2. Vector search in Qdrant (top-15 candidates)
  3. LLM-enhanced ranking via llama3.1:8b with contextual prompt
  4. Return top-3 ranked commands
- **Output format**: Raw commands only (no timestamps/context)
- **Error handling**: Abort gracefully on LLM failure (no fallback to vector similarity)

## Implementation Details

### Embedding Strategy
- **Content**: Raw command text only
- **Timestamp usage**: Store as metadata for future recency scoring (not used in prototype)

### LLM Ranking
- **Input**: Top-15 vector similarity results + user query
- **Output**: Top-3 contextually ranked commands
- **Prompt**: Semantic relevance ranking with command purpose consideration

### Hardcoded Configuration
- **Ollama endpoint**: http://localhost:11434
- **Qdrant endpoint**: http://localhost:6333
- **Collection name**: "zsh_commands"
- **Vector dimensions**: 768 (nomic-embed-text output)

