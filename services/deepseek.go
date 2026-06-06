package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

const DeepSeekBaseURL = "https://api.deepseek.com"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseFormat struct {
	Type string `json:"type"`
}

type ChatRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat ResponseFormat `json:"response_format"`
}

type Choice struct {
	Message Message `json:"message"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// DeepSeekJSON calls the DeepSeek chat completion API in JSON mode and parses the response.
func DeepSeekJSON(ctx context.Context, systemPrompt, userPrompt string, target interface{}) error {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return errors.New("DEEPSEEK_API_KEY belum dikonfigurasi di server")
	}

	reqBody := ChatRequest{
		Model:       "deepseek-chat",
		Temperature: 0.1,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: ResponseFormat{Type: "json_object"},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	url := fmt.Sprintf("%s/chat/completions", DeepSeekBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghubungi layanan AI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("layanan AI menolak permintaan dengan status %d", resp.StatusCode)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return fmt.Errorf("respon AI tidak bisa di-parse: %w", err)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message.Content == "" {
		return errors.New("respon AI tidak berisi pesan")
	}

	content := chatResp.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return fmt.Errorf("AI mengembalikan JSON tidak valid: %w", err)
	}

	return nil
}

// IsDeepSeekConfigured returns true if the DeepSeek API key is present
func IsDeepSeekConfigured() bool {
	return os.Getenv("DEEPSEEK_API_KEY") != ""
}
