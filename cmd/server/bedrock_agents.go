package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type bedrockAgentModelClient struct {
	client *bedrockruntime.Client
}

func newBedrockAgentModelClient(ctx context.Context) (agentModelClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(getEnvDefault("AWS_REGION", "us-east-1")))
	if err != nil {
		return nil, err
	}
	return &bedrockAgentModelClient{client: bedrockruntime.NewFromConfig(cfg)}, nil
}

func (client *bedrockAgentModelClient) CompleteJSON(ctx context.Context, modelID string, system string, prompt string, maxTokens int) ([]byte, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("Bedrock client is not configured")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, fmt.Errorf("Bedrock model id is required")
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body := map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        maxTokens,
		"temperature":       0,
		"system":            system,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	output, err := client.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        raw,
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(output.Body, &response); err != nil {
		return nil, fmt.Errorf("decode Bedrock response: %w", err)
	}
	var buffer bytes.Buffer
	for _, item := range response.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			buffer.WriteString(item.Text)
			buffer.WriteByte('\n')
		}
	}
	if strings.TrimSpace(buffer.String()) == "" {
		return nil, fmt.Errorf("Bedrock response did not include text content")
	}
	return []byte(strings.TrimSpace(buffer.String())), nil
}
