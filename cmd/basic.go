package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// CommandExecutor defines the interface for executing raw commands
type CommandExecutor interface {
	ExecuteCommand(command string, args []string) (interface{}, error)
}

// BasicCommands implements basic Fleare operations
type BasicCommands struct {
	executor CommandExecutor
}

// NewBasicCommands creates a new BasicCommands instance
func NewBasicCommands(executor CommandExecutor) *BasicCommands {
	return &BasicCommands{executor: executor}
}

// Set sets a key-value pair in the database
func (c *BasicCommands) Set(key string, value interface{}) (interface{}, error) {
	args := []string{key}

	// Convert value to string representation
	valueStr, err := c.valueToString(value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to string: %w", err)
	}

	args = append(args, valueStr)
	return c.executor.ExecuteCommand("SET", args)
}

// Get retrieves a value by key, with optional JSON path
func (c *BasicCommands) Get(key string, path ...string) (interface{}, error) {
	args := []string{key}

	if len(path) > 0 && path[0] != "" {
		args = append(args, path[0])
	}

	result, err := c.executor.ExecuteCommand("GET", args)
	if err != nil {
		return nil, err
	}

	return c.parseResult(result)
}

// Get retrieves a value by key, with optional JSON path
func (c *BasicCommands) Ping(value ...string) (string, error) {
	result, err := c.executor.ExecuteCommand("PING", value)
	if err != nil {
		return "", err
	}

	if resultStr, ok := result.(string); ok {
		return resultStr, nil
	}

	return "", fmt.Errorf("unexpected result type for PING command")
}

// Del deletes a key from the database
func (c *BasicCommands) Del(key string) (interface{}, error) {
	args := []string{key}
	return c.executor.ExecuteCommand("DELETE", args)
}

// Exists checks if a key exists in the database
func (c *BasicCommands) Exists(key string) (bool, error) {
	args := []string{key}
	result, err := c.executor.ExecuteCommand("EXISTS", args)
	if err != nil {
		return false, err
	}

	// Convert result to boolean
	if resultStr, ok := result.(string); ok {
		return strconv.ParseBool(resultStr)
	}

	return false, fmt.Errorf("unexpected result type for EXISTS command")
}

// Status gets the server status
func (c *BasicCommands) Status(ctx context.Context) (interface{}, error) {
	result, err := c.executor.ExecuteCommand("STATUS", []string{})
	if err != nil {
		return nil, err
	}

	return c.parseResult(result)
}

// valueToString converts a value to its string representation for transmission
func (c *BasicCommands) valueToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case nil:
		return "", nil
	default:
		// For complex types, marshal to JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("failed to marshal value to JSON: %w", err)
		}
		return string(jsonBytes), nil
	}
}

// parseResult attempts to parse the result into appropriate Go types
func (c *BasicCommands) parseResult(result interface{}) (interface{}, error) {
	if result == nil {
		return nil, nil
	}

	resultStr, ok := result.(string)
	if !ok {
		return result, nil
	}

	if resultStr == "" {
		return nil, nil
	}

	// Try to parse as JSON first
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(resultStr), &jsonResult); err == nil {
		return jsonResult, nil
	}

	// If JSON parsing fails, return as string
	return resultStr, nil
}
