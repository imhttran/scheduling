package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// aiChatRequest is the payload the Admin/Manager screens send when a user asks
// the bundled AI assistant a question. Only the raw question travels from the
// client — the system prompt and model stay server-side (env-configured).
type aiChatRequest struct {
	Message string `json:"message"`
}

// aiChatResponse carries the assistant's answer. `message` is reused by the
// generic API logging in the frontend; `reply` holds the actual content.
type aiChatResponse struct {
	Reply   string `json:"reply"`
	Message string `json:"message,omitempty"`
}

const aiChatTimeout = 120 * time.Second

// aiChat proxies a single user message to any OpenAI-compatible chat-completions
// endpoint (OpenAI, Ollama, LM Studio, etc.). The pre-configured prompt, model,
// base URL and API key all come from the environment (see loadConfig), so no
// credentials ever reach the browser. Only manager/admin roles are allowed in,
// enforced by the route guard in app.go.
func aiChat(w http.ResponseWriter, r *http.Request) {
	if cfg.AIAPIKey == "" || cfg.AIBaseURL == "" || cfg.AIModel == "" {
		respond(w, http.StatusServiceUnavailable, msg(
			"AI assistant is not configured. Set AI_BASE_URL, AI_API_KEY, and AI_MODEL.",
		))
		return
	}

	var req aiChatRequest
	decodeJSON(r, &req)
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		respond(w, http.StatusBadRequest, msg("Message is required"))
		return
	}

	payload := map[string]any{
		"model": cfg.AIModel,
		"messages": []map[string]string{
			{"role": "system", "content": cfg.AIPrompt},
			{"role": "user", "content": req.Message},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		respond500(w, "ai: encode request", err, false)
		return
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		strings.TrimRight(cfg.AIBaseURL, "/")+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		respond500(w, "ai: build request", err, false)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.AIAPIKey)

	client := &http.Client{Timeout: aiChatTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		respond500(w, "ai: call provider", err, false)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("ai: provider returned %d: %s", resp.StatusCode, string(respBody))
		respond(w, http.StatusBadGateway, msg("The AI provider returned an error"))
		return
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		respond500(w, "ai: parse provider response", err, false)
		return
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		respond(w, http.StatusBadGateway, msg("The AI provider returned an empty response"))
		return
	}

	respond(w, http.StatusOK, aiChatResponse{
		Reply:   parsed.Choices[0].Message.Content,
		Message: "AI responded",
	})
}
