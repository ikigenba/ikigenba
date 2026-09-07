package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/model"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

type harness struct {
	cfg Config
}

func runPass(ctx context.Context, cfg Config, prompt string) (Result, error) {
	pass, err := cfg.Store.NextPass()
	if err != nil {
		return Result{}, err
	}
	address := strconv.Itoa(pass)
	runner := harness{cfg: cfg}
	report, err := runner.runAgent(ctx, address, RoleSupervisor, prompt)
	return Result{Address: address, Report: report}, err
}

func (h *harness) runAgent(ctx context.Context, address string, role Role, prompt string) (report string, err error) {
	if _, err := h.cfg.Store.Add(address, store.KindPrompt, prompt, nil); err != nil {
		return "", err
	}

	log := h.newAgentLog(address)
	defer func() {
		err = errors.Join(err, log.Close())
	}()

	conversation, err := h.newConversation(address, role, log)
	if err != nil {
		return "", err
	}
	report, err = h.runConversation(ctx, address, conversation, prompt)
	if err != nil {
		return "", err
	}
	if _, err := h.cfg.Store.Add(address, store.KindReport, report, nil); err != nil {
		return "", err
	}
	return report, nil
}

func (h *harness) newAgentLog(address string) *agentkit.Log {
	writer := store.NewLogWriter(h.cfg.Store, address, h.cfg.Trace.Record)
	return agentkit.NewLog(writer, h.cfg.Now, address)
}

func (h *harness) newConversation(address string, role Role, log *agentkit.Log) (*agentkit.Conversation, error) {
	tools, err := h.tools(address, role)
	if err != nil {
		return nil, err
	}
	factory, system, err := h.role(role)
	if err != nil {
		return nil, err
	}
	conversation, err := factory.New(tools, log)
	if err != nil {
		return nil, err
	}
	if err := conversation.AddSystem(system); err != nil {
		return nil, err
	}
	return conversation, nil
}

func (h *harness) runConversation(
	ctx context.Context,
	address string,
	conversation *agentkit.Conversation,
	prompt string,
) (string, error) {
	var report string
	stream := conversation.Send(ctx, agentkit.Text{Text: prompt})
	for event := range stream.Events() {
		h.cfg.Trace.Event(address, event)
		if done, ok := event.(agentkit.MessageDone); ok && done.Message.Role == agentkit.RoleAssistant {
			report = messageText(done.Message)
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		h.cfg.Trace.Error(address, streamErr)
		return "", streamErr
	}
	return report, nil
}

func (h *harness) role(role Role) (*model.Factory, string, error) {
	switch role {
	case RoleSupervisor:
		if h.cfg.Supervisor == nil {
			return nil, "", errors.New("agent: nil supervisor factory")
		}
		return h.cfg.Supervisor, SupervisorPrompt, nil
	case RoleWorker:
		if h.cfg.Worker == nil {
			return nil, "", errors.New("agent: nil worker factory")
		}
		return h.cfg.Worker, WorkerPrompt, nil
	default:
		return nil, "", fmt.Errorf("agent: unknown role %q", role)
	}
}

func messageText(message agentkit.Message) string {
	parts := make([]string, 0, len(message.Blocks))
	for _, block := range message.Blocks {
		if text, ok := block.(agentkit.Text); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
