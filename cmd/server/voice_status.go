package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const awsCredentialPreflightTimeout = 8 * time.Second

type awsCredentialPreflightResult struct {
	Ready  bool
	Region string
	Error  string
}

type voiceReadinessResponse struct {
	OK                      bool   `json:"ok"`
	Ready                   bool   `json:"ready"`
	VoiceProvider           string `json:"voice_provider"`
	AWSReady                bool   `json:"aws_ready"`
	AgentReady              bool   `json:"agent_ready"`
	AgentParticipantPresent bool   `json:"agent_participant_present"`
	BedrockStreamActive     bool   `json:"bedrock_stream_active"`
	TranscriptionFlowing    bool   `json:"transcription_flowing"`
	LastTranscriptionAt     string `json:"last_transcription_at,omitempty"`
	JiraReady               bool   `json:"jira_ready"`
	Region                  string `json:"region,omitempty"`
	RoomID                  string `json:"room_id"`
	BoardID                 string `json:"board_id"`
	Message                 string `json:"message"`
	RecoveryCommand         string `json:"recovery_command,omitempty"`
	RequiresRestart         bool   `json:"requires_restart,omitempty"`
	LastError               string `json:"last_error,omitempty"`
}

var validateAWSRuntimeCredentials = defaultValidateAWSRuntimeCredentials

func voiceStatusHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authCtx, ok := authorizeRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	status := currentVoiceReadiness(r.Context(), true)
	status.RoomID = authCtx.RoomID
	status.BoardID = authCtx.BoardID
	writeJSON(w, http.StatusOK, status)
}

func currentVoiceReadiness(ctx context.Context, ensureAgent bool) voiceReadinessResponse {
	provider := strings.TrimSpace(voiceProvider)
	if provider == "" {
		provider = "openai"
	}

	status := voiceReadinessResponse{
		OK:            true,
		Ready:         true,
		VoiceProvider: provider,
		AWSReady:      true,
		AgentReady:    true,
		JiraReady:     jiraSync != nil,
		Region:        getEnvDefault("AWS_REGION", "us-east-1"),
		RoomID:        appRoomID,
		BoardID:       appBoardID,
		Message:       "Voice agent is ready.",
	}
	if sharedBoard != nil {
		status.LastTranscriptionAt = sharedBoard.LastTranscriptAt()
		status.TranscriptionFlowing = status.LastTranscriptionAt != ""
	}

	if provider != "nova-sonic" {
		status.AgentParticipantPresent = true
		return status
	}

	status.Ready = false
	status.AWSReady = false
	status.AgentReady = false
	status.AgentParticipantPresent = false

	awsStatus := validateAWSRuntimeCredentials(ctx)
	if awsStatus.Region != "" {
		status.Region = awsStatus.Region
	}
	if !awsStatus.Ready {
		status.LastError = awsStatus.Error
		status.Message = localAWSRecoveryMessage(awsStatus.Error)
		status.RecoveryCommand = "scripts/local-up.sh"
		status.RequiresRestart = true
		return status
	}
	status.AWSReady = true

	if novaSonic == nil {
		status.Message = "Nova Sonic is configured, but the voice agent has not been initialized."
		return status
	}

	if ensureAgent && !novaSonic.IsConnected() {
		if err := novaSonic.JoinConferenceRoom(); err != nil {
			status.LastError = scrubStatusError(err)
			status.Message = "AWS credentials are valid, but Nova Sonic could not join LiveKit. Check Docker logs for the app and LiveKit containers, then try Join Room again."
			return status
		}
	}

	status.AgentReady = novaSonic.IsConnected()
	status.AgentParticipantPresent = status.AgentReady
	status.BedrockStreamActive = novaSonic.StreamActive()
	if !status.AgentReady {
		status.LastError = novaSonic.LastJoinError()
		status.Message = "AWS credentials are valid, but Nova Sonic is still connecting to LiveKit. Wait a moment, then click Join Room again."
		return status
	}

	status.Ready = true
	status.Message = "Nova Sonic voice agent is ready."
	return status
}

func defaultValidateAWSRuntimeCredentials(ctx context.Context) awsCredentialPreflightResult {
	preflightCtx, cancel := context.WithTimeout(ctx, awsCredentialPreflightTimeout)
	defer cancel()

	cfg, region, err := resolveAWSRuntimeConfig(preflightCtx)
	if err != nil {
		return awsCredentialPreflightResult{
			Ready:  false,
			Region: region,
			Error:  scrubStatusError(fmt.Errorf("load AWS config: %w", err)),
		}
	}

	if err := validateAWSConfigIdentity(preflightCtx, cfg); err != nil {
		return awsCredentialPreflightResult{
			Ready:  false,
			Region: region,
			Error:  scrubStatusError(fmt.Errorf("validate AWS credentials: %w", err)),
		}
	}

	return awsCredentialPreflightResult{Ready: true, Region: region}
}

func resolveAWSRuntimeConfig(ctx context.Context) (aws.Config, string, error) {
	region := getEnvDefault("AWS_REGION", "us-east-1")

	cfgOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")) == "" {
		profile := getEnvDefault("AWS_PROFILE", "test_AccountA/AdministratorAccess")
		cfgOpts = append(cfgOpts, awsconfig.WithSharedConfigProfile(profile))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, cfgOpts...)
	return cfg, region, err
}

func validateAWSConfigIdentity(ctx context.Context, cfg aws.Config) error {
	_, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	return err
}

func localAWSRecoveryMessage(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "AWS credentials are missing or expired."
	}
	return reason + " Run scripts/local-up.sh from this repo; it will run assume test_AccountA/AdministratorAccess in us-east-1, restart Docker with fresh temporary AWS credentials, and reopen the app. Then click Join Room again."
}

func scrubStatusError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if len(msg) > 700 {
		msg = msg[:700] + "..."
	}
	return msg
}
