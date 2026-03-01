// === ./pkg/ai/providers/mistral/qna.go ===
package aimistral

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Abraxas-365/manifesto/pkg/ai/ocr"
)

// ============================================================================
// DocumentQnA Implementation
// ============================================================================

// AskQuestion implements single question answering
func (m *MistralProvider) AskQuestion(ctx context.Context, input ocr.Input, question string, opts ...ocr.Option) (ocr.QnAResponse, error) {
	options := ocr.ApplyOptions(opts...)

	model := m.defaultChatModel
	if options.Model != "" {
		model = options.Model
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return ocr.QnAResponse{}, err
	}

	if question == "" {
		return ocr.QnAResponse{}, errorRegistry.New(ErrInvalidInput).
			WithDetail("error", "question cannot be empty")
	}

	// Build chat completion request
	req := &ChatRequest{
		Model: model,
		Messages: []ChatMessage{
			{
				Role: "user",
				Content: []ContentPart{
					{
						Type: "text",
						Text: question,
					},
					m.buildDocumentContent(input),
				},
			},
		},
	}

	// Call chat completion API
	respBody, err := m.client.Post(ctx, "/chat/completions", req)
	if err != nil {
		return ocr.QnAResponse{}, err
	}

	// Parse response
	var resp ChatResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return ocr.QnAResponse{}, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse chat response")
	}

	if len(resp.Choices) == 0 {
		return ocr.QnAResponse{}, errorRegistry.New(ErrAPIResponse).
			WithDetail("error", "no choices in response")
	}

	return ocr.QnAResponse{
		Answer: resp.Choices[0].Message.Content,
		TokenUsage: ocr.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// AskQuestions implements multiple question answering
func (m *MistralProvider) AskQuestions(ctx context.Context, input ocr.Input, questions []string, opts ...ocr.Option) ([]ocr.QnAResponse, error) {
	if len(questions) == 0 {
		return nil, errorRegistry.New(ErrInvalidInput).
			WithDetail("error", "questions list cannot be empty")
	}

	responses := make([]ocr.QnAResponse, len(questions))

	for i, question := range questions {
		resp, err := m.AskQuestion(ctx, input, question, opts...)
		if err != nil {
			return nil, WrapError(err, ErrProcessingFailed).
				WithDetail("question_index", i).
				WithDetail("question", question)
		}
		responses[i] = resp
	}

	return responses, nil
}

// Chat implements multi-turn conversation
func (m *MistralProvider) Chat(ctx context.Context, input ocr.Input, messages []ocr.ConversationMessage, opts ...ocr.Option) (ocr.QnAResponse, error) {
	options := ocr.ApplyOptions(opts...)

	model := m.defaultChatModel
	if options.Model != "" {
		model = options.Model
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return ocr.QnAResponse{}, err
	}

	if len(messages) == 0 {
		return ocr.QnAResponse{}, errorRegistry.New(ErrInvalidInput).
			WithDetail("error", "messages list cannot be empty")
	}

	// Convert conversation messages
	chatMessages := make([]ChatMessage, len(messages))

	// First message includes the document
	chatMessages[0] = ChatMessage{
		Role: messages[0].Role,
		Content: []ContentPart{
			{
				Type: "text",
				Text: messages[0].Content,
			},
			m.buildDocumentContent(input),
		},
	}

	// Subsequent messages are text-only
	for i := 1; i < len(messages); i++ {
		chatMessages[i] = ChatMessage{
			Role: messages[i].Role,
			Content: []ContentPart{
				{
					Type: "text",
					Text: messages[i].Content,
				},
			},
		}
	}

	req := &ChatRequest{
		Model:    model,
		Messages: chatMessages,
	}

	// Call chat completion API
	respBody, err := m.client.Post(ctx, "/chat/completions", req)
	if err != nil {
		return ocr.QnAResponse{}, err
	}

	// Parse response
	var resp ChatResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return ocr.QnAResponse{}, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse chat response")
	}

	if len(resp.Choices) == 0 {
		return ocr.QnAResponse{}, errorRegistry.New(ErrAPIResponse).
			WithDetail("error", "no choices in response")
	}

	return ocr.QnAResponse{
		Answer: resp.Choices[0].Message.Content,
		TokenUsage: ocr.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (m *MistralProvider) buildDocumentContent(input ocr.Input) ContentPart {
	switch input.Type {
	case ocr.InputTypeURL, ocr.InputTypeDocumentURL:
		return ContentPart{
			Type:        "document_url",
			DocumentURL: input.URL,
		}
	case ocr.InputTypeImageURL:
		return ContentPart{
			Type:     "image_url",
			ImageURL: input.URL,
		}
	case ocr.InputTypeBase64:
		mimeType := input.MimeType
		if mimeType == "" {
			mimeType = "application/pdf"
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, string(input.Data))
		return ContentPart{
			Type:        "document_url",
			DocumentURL: dataURL,
		}
	default:
		return ContentPart{
			Type:        "document_url",
			DocumentURL: input.URL,
		}
	}
}
